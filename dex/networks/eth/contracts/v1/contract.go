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
	Bin: "0x60a06040523480156200001157600080fd5b506040516200251438038062002514833981016040819052620000349162000099565b60016000556001600160a01b038116620000875760405162461bcd60e51b815260206004820152601060248201526f1e995c9bc8195b9d1c9e481c1bda5b9d60821b604482015260640160405180910390fd5b6001600160a01b0316608052620000cb565b600060208284031215620000ac57600080fd5b81516001600160a01b0381168114620000c457600080fd5b9392505050565b60805161241f620000f5600039600081816101f6015281816102e40152610cc4015261241f6000f3fe6080604052600436106100d25760003560e01c8063950bff9f1161007f578063d0758daf11610059578063d0758daf14610230578063eb84e7f21461025d578063f0e3b8d51461028a578063f9f2e0f4146102b757600080fd5b8063950bff9f146101af5780639ef07b4c146101c4578063b0d691fe146101e457600080fd5b806368356d75116100b057806368356d751461014f57806371b6d0111461016f57806377d7e0311461018f57600080fd5b806319822f7c146100d757806333a3bcb41461010a57806352145bc01461013a575b600080fd5b3480156100e357600080fd5b506100f76100f2366004611d63565b6102d7565b6040519081526020015b60405180910390f35b34801561011657600080fd5b5061012a610125366004611dd3565b6106fe565b6040519015158152602001610101565b61014d610148366004611e12565b610751565b005b34801561015b57600080fd5b5061014d61016a366004611ee4565b610cb9565b34801561017b57600080fd5b5061014d61018a366004611dd3565b611125565b34801561019b57600080fd5b5061012a6101aa366004611f30565b611439565b3480156101bb57600080fd5b506100f7601481565b3480156101d057600080fd5b506100f76101df36600461205f565b6114b3565b3480156101f057600080fd5b506102187f000000000000000000000000000000000000000000000000000000000000000081565b6040516001600160a01b039091168152602001610101565b34801561023c57600080fd5b506100f761024b366004612093565b60026020526000908152604090205481565b34801561026957600080fd5b506100f7610278366004612093565b60016020526000908152604090205481565b34801561029657600080fd5b506102aa6102a5366004611dd3565b6115a6565b60405161010191906120c2565b3480156102c357600080fd5b5061014d6102d2366004612105565b611647565b6000336001600160a01b037f000000000000000000000000000000000000000000000000000000000000000016146103565760405162461bcd60e51b815260206004820152600e60248201527f6e6f7420656e747279506f696e7400000000000000000000000000000000000060448201526064015b60405180910390fd5b61035e611a46565b600461036d6060860186612158565b9050101561037d575060016106ed565b7f68356d75000000000000000000000000000000000000000000000000000000006103ab6060860186612158565b6103ba9160049160009161219f565b6103c3916121c9565b7fffffffff0000000000000000000000000000000000000000000000000000000016146103f2575060016106ed565b6000806104026060870187612158565b61041091600490829061219f565b81019061041d919061220f565b9150915085602001358114610437576001925050506106ed565b81511580610446575060148251115b15610456576001925050506106ed565b60008060005b84518110156105c7576000858281518110610479576104796122e6565b602002602001015190506000801b8160200151036104a057600196505050505050506106ed565b60208101518151516104b29190611439565b6104c557600196505050505050506106ed565b60008060006104d960008560000151611a89565b600083815260036020526040902054929550909350915043900361050957600199505050505050505050506106ed565b8015806105165750438110155b80610522575060001982145b806105365750835151610536908390611439565b1561054d57600199505050505050505050506106ed565b60008381526003602052604081204390558590036105725783516080015196506105a5565b866001600160a01b03168460000151608001516001600160a01b0316146105a557600199505050505050505050506106ed565b8351602001516105b59087612312565b9550506001909301925061045c915050565b5060006161a885516105d99190612325565b6105e690620124f8612312565b90506fffffffffffffffffffffffffffffffff60808a013516811115610614576001955050505050506106ed565b8187111561062a576001955050505050506106ed565b6000806106798a61063f6101008e018e612158565b8080601f016020809104026020016040519081016040528093929190818152602001838380828437600092019190915250611ab592505050565b5090925090506000816003811115610693576106936120ac565b1415806106b25750846001600160a01b0316826001600160a01b031614155b156106c75760019750505050505050506106ed565b60008681526002602052604090208990556106e189611b02565b60009750505050505050505b6106f76001600055565b9392505050565b6000808061071a856107153687900387018761233c565b611a89565b92509250508060001415801561073257506000198214155b80156107465750610744828535611439565b155b925050505b92915050565b3233146107935760405162461bcd60e51b815260206004820152601060248201526f39b2b73232b910109e9037b934b3b4b760811b604482015260640161034d565b61079b611a46565b80158015906107ab575060148111155b6107e85760405162461bcd60e51b815260206004820152600e60248201526d6261642062617463682073697a6560901b604482015260640161034d565b6000805b82811015610be35736848483818110610807576108076122e6565b905060a00201905060008160200135116108635760405162461bcd60e51b815260206004820152600a60248201527f7a65726f2076616c756500000000000000000000000000000000000000000000604482015260640161034d565b60006108756060830160408401612358565b6001600160a01b0316141580156108a55750600061089960a0830160808401612358565b6001600160a01b031614155b6108f15760405162461bcd60e51b815260206004820152600960248201527f7a65726f20616464720000000000000000000000000000000000000000000000604482015260640161034d565b426109026080830160608401612373565b67ffffffffffffffff16116109595760405162461bcd60e51b815260206004820152600f60248201527f62616420726566756e642074696d650000000000000000000000000000000000604482015260640161034d565b80356109a75760405162461bcd60e51b815260206004820152600960248201527f7a65726f20686173680000000000000000000000000000000000000000000000604482015260640161034d565b7f5069ec89f08d9ca0424bb5a5f59c3c60ed50cf06af5911a368e41e771763bfaf813501610a175760405162461bcd60e51b815260206004820152600c60248201527f696c6c6567616c20686173680000000000000000000000000000000000000000604482015260640161034d565b7f99978552079d428893703e74716071dff768eb7a911dcc4c6fd5a6e2f2a0d6db813501610a875760405162461bcd60e51b815260206004820152601060248201527f7a65726f20736563726574206861736800000000000000000000000000000000604482015260640161034d565b6000610a9c876101df3685900385018561233c565b60008181526001602052604090205490915015610afb5760405162461bcd60e51b815260206004820152600e60248201527f616c726561647920657869737473000000000000000000000000000000000000604482015260640161034d565b43610b07818435611439565b15610b545760405162461bcd60e51b815260206004820152600e60248201527f6861736820636f6c6c6973696f6e000000000000000000000000000000000000604482015260640161034d565b6000828152600160209081526040909120829055610b759084013586612312565b9450610b876060840160408501612358565b6001600160a01b0316886001600160a01b0316837fe6cf32272b10a93a5a022b8a3184f4c213f232ce976cb7cc95190fa8d3c9d5468660200135604051610bd091815260200190565b60405180910390a45050506001016107ec565b506001600160a01b038416610c4657803414610c415760405162461bcd60e51b815260206004820152600d60248201527f626164204554482076616c756500000000000000000000000000000000000000604482015260640161034d565b610ca9565b3415610c945760405162461bcd60e51b815260206004820152601560248201527f6e6f2045544820666f7220746f6b656e20737761700000000000000000000000604482015260640161034d565b610ca96001600160a01b038516333084611b57565b50610cb46001600055565b505050565b336001600160a01b037f00000000000000000000000000000000000000000000000000000000000000001614610d315760405162461bcd60e51b815260206004820152600e60248201527f6e6f7420656e747279506f696e74000000000000000000000000000000000000604482015260640161034d565b610d39611a46565b8115801590610d49575060148211155b610d865760405162461bcd60e51b815260206004820152600e60248201526d6261642062617463682073697a6560901b604482015260640161034d565b60008060005b848110156110555736868683818110610da757610da76122e6565b905060c00201905081600003610dce57610dc760a0820160808301612358565b9250610e3e565b6001600160a01b038316610de860a0830160808401612358565b6001600160a01b031614610e3e5760405162461bcd60e51b815260206004820152601460248201527f7061727469636970616e74206d69736d61746368000000000000000000000000604482015260640161034d565b60008080610e55816107153687900387018761233c565b925092509250600081118015610e6a57504381105b610eb65760405162461bcd60e51b815260206004820152600e60248201527f6e6f742072656465656d61626c65000000000000000000000000000000000000604482015260640161034d565b60018201610ef95760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c99599d5b99195960821b604482015260640161034d565b610f04828535611439565b15610f445760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b604482015260640161034d565b60a0840135610f835760405162461bcd60e51b815260206004820152600b60248201526a1e995c9bc81cd958dc995d60aa1b604482015260640161034d565b610f9260a08501358535611439565b610fcb5760405162461bcd60e51b815260206004820152600a602482015269189859081cd958dc995d60b21b604482015260640161034d565b600083815260016020908152604090912060a08601359055610ff09085013588612312565b9650856001600160a01b031660006001600160a01b0316847fd2612260761e59ccc782befe6e363c6e3a99cff88fd779a4e8df97622303645f8760a0013560405161103d91815260200190565b60405180910390a4505060019092019150610d8c9050565b50600083815260026020526040812080549082905590611075828561238e565b90506000836001600160a01b03168260405160006040518083038185875af1925050503d80600081146110c4576040519150601f19603f3d011682016040523d82523d6000602084013e6110c9565b606091505b505090508061111657836001600160a01b03167f29567697919cf5773444f8d73a44c6de6f6cd28f5fe7423b508a4c2e41039ae08360405161110d91815260200190565b60405180910390a25b5050505050610cb46001600055565b3233146111675760405162461bcd60e51b815260206004820152601060248201526f39b2b73232b910109e9037b934b3b4b760811b604482015260640161034d565b61116f611a46565b61117f6080820160608301612373565b67ffffffffffffffff164210156111d85760405162461bcd60e51b815260206004820152600b60248201527f6e6f742065787069726564000000000000000000000000000000000000000000604482015260640161034d565b600080806111ef856107153687900387018761233c565b925092509250600081116112455760405162461bcd60e51b815260206004820152600d60248201527f6e6f7420696e6974696174656400000000000000000000000000000000000000604482015260640161034d565b600182016112885760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c99599d5b99195960821b604482015260640161034d565b611293828535611439565b156112d35760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b604482015260640161034d565b600083815260016020526040902060001990556001600160a01b0385166113b05760006113066060860160408701612358565b6001600160a01b0316856020013560405160006040518083038185875af1925050503d8060008114611354576040519150601f19603f3d011682016040523d82523d6000602084013e611359565b606091505b50509050806113aa5760405162461bcd60e51b815260206004820152601160248201527f45544820726566756e64206661696c6564000000000000000000000000000000604482015260640161034d565b506113d8565b6113d86113c36060860160408701612358565b6001600160a01b038716906020870135611bd9565b6113e86060850160408601612358565b6001600160a01b0316856001600160a01b0316847f2aff553948b882cad400e03aba914ec778ac8c4698954c03e145c1186338ad6360405160405180910390a45050506114356001600055565b5050565b60008160028460405160200161145191815260200190565b60408051601f198184030181529082905261146b916123a1565b602060405180830381855afa158015611488573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906114ab91906123d0565b149392505050565b600060028260000151836040015160601b846080015160601b856020015160001b866060015160c01b8860601b60405160200161154c969594939291909586526bffffffffffffffffffffffff199485166020870152928416603486015260488501919091527fffffffffffffffff00000000000000000000000000000000000000000000000016606884015216607082015260840190565b60408051601f1981840301815290829052611566916123a1565b602060405180830381855afa158015611583573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906106f791906123d0565b6040805160608101825260008082526020820181905291810182905290806115d7856107153687900387018761233c565b925092505080600003611603578260005b908160038111156115fb576115fb6120ac565b90525061163f565b60018201611613578260036115e8565b61161e828535611439565b1561163357600283526020830182905261163f565b60018352604083018190525b505092915050565b3233146116895760405162461bcd60e51b815260206004820152601060248201526f39b2b73232b910109e9037b934b3b4b760811b604482015260640161034d565b611691611a46565b80158015906116a1575060148111155b6116de5760405162461bcd60e51b815260206004820152600e60248201526d6261642062617463682073697a6560901b604482015260640161034d565b6000805b8281101561198557368484838181106116fd576116fd6122e6565b60c00291909101915033905061171960a0830160808401612358565b6001600160a01b03161461176f5760405162461bcd60e51b815260206004820152600f60248201527f6e6f74207061727469636970616e740000000000000000000000000000000000604482015260640161034d565b60008080611786896107153687900387018761233c565b92509250925060008111801561179b57504381105b6117e75760405162461bcd60e51b815260206004820152600e60248201527f6e6f742072656465656d61626c65000000000000000000000000000000000000604482015260640161034d565b6001820161182a5760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c99599d5b99195960821b604482015260640161034d565b611835828535611439565b156118755760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b604482015260640161034d565b60a08401356118b45760405162461bcd60e51b815260206004820152600b60248201526a1e995c9bc81cd958dc995d60aa1b604482015260640161034d565b6118c360a08501358535611439565b6118fc5760405162461bcd60e51b815260206004820152600a602482015269189859081cd958dc995d60b21b604482015260640161034d565b600083815260016020908152604090912060a086013590556119219085013587612312565b9550336001600160a01b0316896001600160a01b0316847fd2612260761e59ccc782befe6e363c6e3a99cff88fd779a4e8df97622303645f8760a0013560405161196d91815260200190565b60405180910390a45050600190920191506116e29050565b506001600160a01b038416611a3257604051600090339083908381818185875af1925050503d80600081146119d6576040519150601f19603f3d011682016040523d82523d6000602084013e6119db565b606091505b5050905080611a2c5760405162461bcd60e51b815260206004820152601360248201527f455448207472616e73666572206661696c656400000000000000000000000000604482015260640161034d565b50610ca9565b610ca96001600160a01b0385163383611bd9565b600260005403611a82576040517f3ee5aeb500000000000000000000000000000000000000000000000000000000815260040160405180910390fd5b6002600055565b6000806000611a9885856114b3565b600081815260016020526040902054909690955085945092505050565b60008060008351604103611aef5760208401516040850151606086015160001a611ae188828585611c0a565b955095509550505050611afb565b50508151600091506002905b9250925092565b8015611b5457604051600090339083908381818185875af1925050503d8060008114611b4a576040519150601f19603f3d011682016040523d82523d6000602084013e611b4f565b606091505b505050505b50565b6040516001600160a01b038481166024830152838116604483015260648201839052611bd39186918216906323b872dd906084015b604051602081830303815290604052915060e01b6020820180517bffffffffffffffffffffffffffffffffffffffffffffffffffffffff8381831617835250505050611cd9565b50505050565b6040516001600160a01b03838116602483015260448201839052610cb491859182169063a9059cbb90606401611b8c565b600080807f7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0841115611c455750600091506003905082611ccf565b604080516000808252602082018084528a905260ff891692820192909252606081018790526080810186905260019060a0016020604051602081039080840390855afa158015611c99573d6000803e3d6000fd5b5050604051601f1901519150506001600160a01b038116611cc557506000925060019150829050611ccf565b9250600091508190505b9450945094915050565b600080602060008451602086016000885af180611cfc576040513d6000823e3d81fd5b50506000513d91508115611d14578060011415611d21565b6001600160a01b0384163b155b15611bd3576040517f5274afe70000000000000000000000000000000000000000000000000000000081526001600160a01b038516600482015260240161034d565b600080600060608486031215611d7857600080fd5b833567ffffffffffffffff811115611d8f57600080fd5b84016101208187031215611da257600080fd5b95602085013595506040909401359392505050565b80356001600160a01b0381168114611dce57600080fd5b919050565b60008082840360c0811215611de757600080fd5b611df084611db7565b925060a0601f1982011215611e0457600080fd5b506020830190509250929050565b600080600060408486031215611e2757600080fd5b611e3084611db7565b9250602084013567ffffffffffffffff80821115611e4d57600080fd5b818601915086601f830112611e6157600080fd5b813581811115611e7057600080fd5b87602060a083028501011115611e8557600080fd5b6020830194508093505050509250925092565b60008083601f840112611eaa57600080fd5b50813567ffffffffffffffff811115611ec257600080fd5b60208301915083602060c083028501011115611edd57600080fd5b9250929050565b600080600060408486031215611ef957600080fd5b833567ffffffffffffffff811115611f1057600080fd5b611f1c86828701611e98565b909790965060209590950135949350505050565b60008060408385031215611f4357600080fd5b50508035926020909101359150565b634e487b7160e01b600052604160045260246000fd5b6040805190810167ffffffffffffffff81118282101715611f8b57611f8b611f52565b60405290565b604051601f8201601f1916810167ffffffffffffffff81118282101715611fba57611fba611f52565b604052919050565b803567ffffffffffffffff81168114611dce57600080fd5b600060a08284031215611fec57600080fd5b60405160a0810181811067ffffffffffffffff8211171561200f5761200f611f52565b8060405250809150823581526020830135602082015261203160408401611db7565b604082015261204260608401611fc2565b606082015261205360808401611db7565b60808201525092915050565b60008060c0838503121561207257600080fd5b61207b83611db7565b915061208a8460208501611fda565b90509250929050565b6000602082840312156120a557600080fd5b5035919050565b634e487b7160e01b600052602160045260246000fd5b81516060820190600481106120e757634e487b7160e01b600052602160045260246000fd5b80835250602083015160208301526040830151604083015292915050565b60008060006040848603121561211a57600080fd5b61212384611db7565b9250602084013567ffffffffffffffff81111561213f57600080fd5b61214b86828701611e98565b9497909650939450505050565b6000808335601e1984360301811261216f57600080fd5b83018035915067ffffffffffffffff82111561218a57600080fd5b602001915036819003821315611edd57600080fd5b600080858511156121af57600080fd5b838611156121bc57600080fd5b5050820193919092039150565b7fffffffff00000000000000000000000000000000000000000000000000000000813581811691600485101561163f5760049490940360031b84901b1690921692915050565b6000806040838503121561222257600080fd5b823567ffffffffffffffff8082111561223a57600080fd5b818501915085601f83011261224e57600080fd5b813560208282111561226257612262611f52565b612270818360051b01611f91565b828152818101935060c092830285018201928984111561228f57600080fd5b948201945b838610156122d85780868b0312156122ac5760008081fd5b6122b4611f68565b6122be8b88611fda565b815260a08701358482015285529485019493820193612294565b509896013596505050505050565b634e487b7160e01b600052603260045260246000fd5b634e487b7160e01b600052601160045260246000fd5b8082018082111561074b5761074b6122fc565b808202811582820484141761074b5761074b6122fc565b600060a0828403121561234e57600080fd5b6106f78383611fda565b60006020828403121561236a57600080fd5b6106f782611db7565b60006020828403121561238557600080fd5b6106f782611fc2565b8181038181111561074b5761074b6122fc565b6000825160005b818110156123c257602081860181015185830152016123a8565b506000920191825250919050565b6000602082840312156123e257600080fd5b505191905056fea264697066735822122032db92972a6e0cfad20131df8a3297c5010ba6a14698015e9be15044fd70549464736f6c63430008170033",
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
