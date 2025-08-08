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
	ABI: "[{\"inputs\":[{\"internalType\":\"addresspayable\",\"name\":\"_entryPoint\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"contractKey\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"entryPoint\",\"outputs\":[{\"internalType\":\"addresspayable\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector[]\",\"name\":\"contracts\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeemAA\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"redeemPrepayments\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"secretValidates\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"status\",\"outputs\":[{\"components\":[{\"internalType\":\"enumETHSwap.Step\",\"name\":\"step\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"internalType\":\"structETHSwap.Status\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"initCode\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"callGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"verificationGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"preVerificationGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxPriorityFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"paymasterAndData\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"internalType\":\"structUserOperation\",\"name\":\"userOp\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"userOpHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"missingAccountFunds\",\"type\":\"uint256\"}],\"name\":\"validateUserOp\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"validationData\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561001057600080fd5b50604051611f06380380611f0683398101604081905261002f91610054565b600080546001600160a01b0319166001600160a01b0392909216919091179055610084565b60006020828403121561006657600080fd5b81516001600160a01b038116811461007d57600080fd5b9392505050565b611e73806100936000396000f3fe6080604052600436106100a75760003560e01c80639ef07b4c116100645780639ef07b4c14610184578063b0d691fe146101a4578063eb84e7f2146101dc578063f0e3b8d514610209578063f521b2eb14610236578063f9f2e0f41461026357600080fd5b806333a3bcb4146100ac5780633a871cdd146100e157806352145bc01461010f57806371b6d0111461012457806377d7e03114610144578063919835a414610164575b600080fd5b3480156100b857600080fd5b506100cc6100c7366004611792565b610283565b60405190151581526020015b60405180910390f35b3480156100ed57600080fd5b506101016100fc3660046117d1565b6102c8565b6040519081526020016100d8565b61012261011d366004611824565b61053e565b005b34801561013057600080fd5b5061012261013f366004611792565b6108ec565b34801561015057600080fd5b506100cc61015f3660046118a9565b610c07565b34801561017057600080fd5b5061012261017f36600461190f565b610c81565b34801561019057600080fd5b5061010161019f366004611a59565b610fa1565b3480156101b057600080fd5b506000546101c4906001600160a01b031681565b6040516001600160a01b0390911681526020016100d8565b3480156101e857600080fd5b506101016101f7366004611a8d565b60016020526000908152604090205481565b34801561021557600080fd5b50610229610224366004611792565b61107c565b6040516100d89190611abc565b34801561024257600080fd5b50610101610251366004611a8d565b60026020526000908152604090205481565b34801561026f57600080fd5b5061012261027e366004611aff565b611142565b6000808061029f8561029a36879003870187611b51565b611481565b9250925050806000141580156102bd57506102bb828535610c07565b155b925050505b92915050565b600080546001600160a01b0316331461031f5760405162461bcd60e51b81526020600482015260146024820152731cd95b99195c88084f48195b9d1c9e541bda5b9d60621b60448201526064015b60405180910390fd5b600461032e6060860186611b6d565b9050101561033e57506001610537565b6324660d6960e21b6103536060860186611b6d565b61036291600491600091611bb3565b61036b91611bdd565b6001600160e01b0319161461038257506002610537565b60006103916060860186611b6d565b61039f916004908290611bb3565b8101906103ac9190611c0d565b905060008060005b83518110156104a15760008482815181106103d1576103d1611cdf565b602002602001015190506000806103ed60008460000151611481565b9250925050806000148061040a575082515161040a908390610c07565b1561041f576003975050505050505050610537565b836000036104485782516080810151905160009081526002602052604090208a90559550610479565b856001600160a01b03168360000151608001516001600160a01b031614610479576004975050505050505050610537565b8251602001516104899086611d0b565b9450505050808061049990611d1e565b9150506103b4565b50808511156104b65760059350505050610537565b610502866104c86101408a018a611b6d565b8080601f0160208091040260200160405190810160405280939291908181526020018383808284376000920191909152506114af92505050565b6001600160a01b0316826001600160a01b0316146105265760069350505050610537565b61052f856114d3565b600093505050505b9392505050565b32331461055d5760405162461bcd60e51b815260040161031690611d37565b6000805b82811015610787573684848381811061057c5761057c611cdf565b905060a00201905060008160200135116105c05760405162461bcd60e51b81526020600482015260056024820152640c081d985b60da1b6044820152606401610316565b60006105d26080830160608401611d61565b6001600160401b03161161061c5760405162461bcd60e51b815260206004820152601160248201527003020726566756e6454696d657374616d7607c1b6044820152606401610316565b7f5069ec89f08d9ca0424bb5a5f59c3c60ed50cf06af5911a368e41e771763bfaf81350161069d5760405162461bcd60e51b815260206004820152602860248201527f696c6c6567616c2073656372657420686173682028726566756e64207265636f604482015267726420686173682960c01b6064820152608401610316565b60006106b28761019f36859003850185611b51565b60008181526001602052604090205490915080156107035760405162461bcd60e51b815260206004820152600e60248201526d73776170206e6f7420656d70747960901b6044820152606401610316565b5043610710818435610c07565b1561074e5760405162461bcd60e51b815260206004820152600e60248201526d3430b9b41031b7b63634b9b4b7b760911b6044820152606401610316565b600082815260016020908152604090912082905561076f9084013586611d0b565b9450505050808061077f90611d1e565b915050610561565b506001600160a01b0384166107d4573481146107cf5760405162461bcd60e51b8152602060048201526007602482015266189859081d985b60ca1b6044820152606401610316565b6108e6565b60408051336024820152306044820152606480820184905282518083039091018152608490910182526020810180516001600160e01b03166323b872dd60e01b17905290516000916060916001600160a01b0388169161083391611d7c565b6000604051808303816000865af19150503d8060008114610870576040519150601f19603f3d011682016040523d82523d6000602084013e610875565b606091505b5090925090508180156108a05750805115806108a05750808060200190518101906108a09190611dab565b6108e35760405162461bcd60e51b81526020600482015260146024820152731d1c985b9cd9995c88199c9bdb4819985a5b195960621b6044820152606401610316565b50505b50505050565b32331461090b5760405162461bcd60e51b815260040161031690611d37565b61091b6080820160608301611d61565b6001600160401b031642101561096a5760405162461bcd60e51b81526020600482015260146024820152731b1bd8dadd1a5b59481b9bdd08195e1c1a5c995960621b6044820152606401610316565b600080806109818561029a36879003870187611b51565b9250925092506000811180156109975750438111155b6109d55760405162461bcd60e51b815260206004820152600f60248201526e73776170206e6f742061637469766560881b6044820152606401610316565b6109e0828535610c07565b15610a255760405162461bcd60e51b81526020600482015260156024820152741cddd85c08185b1c9958591e481c995919595b5959605a1b6044820152606401610316565b600083815260016020526040902060001990556001600160a01b038516610ad8576000610a586060860160408701611dcd565b6001600160a01b0316856020013560405160006040518083038185875af1925050503d8060008114610aa6576040519150601f19603f3d011682016040523d82523d6000602084013e610aab565b606091505b5090915050600181151514610ad25760405162461bcd60e51b815260040161031690611de8565b50610c00565b600060606001600160a01b0387167fa9059cbb2ab09eb219583f4a59a5d0623ade346d962bcd4e46b11da047c9049b610b1688840160408a01611dcd565b6040516001600160a01b0390911660248201526020890135604482015260640160408051601f198184030181529181526020820180516001600160e01b03166001600160e01b0319909416939093179092529051610b749190611d7c565b6000604051808303816000865af19150503d8060008114610bb1576040519150601f19603f3d011682016040523d82523d6000602084013e610bb6565b606091505b509092509050818015610be1575080511580610be1575080806020019051810190610be19190611dab565b610bfd5760405162461bcd60e51b815260040161031690611de8565b50505b5050505050565b600081600284604051602001610c1f91815260200190565b60408051601f1981840301815290829052610c3991611d7c565b602060405180830381855afa158015610c56573d6000803e3d6000fd5b5050506040513d601f19601f82011682018060405250810190610c799190611e11565b149392505050565b6000546001600160a01b03163314610cd25760405162461bcd60e51b81526020600482015260146024820152731cd95b99195c88084f48195b9d1c9e541bda5b9d60621b6044820152606401610316565b600080805b83811015610eb85736858583818110610cf257610cf2611cdf565b905060c00201905081600003610d1957610d1260a0820160808301611dcd565b9250610d7b565b6001600160a01b038316610d3360a0830160808401611dcd565b6001600160a01b031614610d7b5760405162461bcd60e51b815260206004820152600f60248201526e189859081c185c9d1a58da5c185b9d608a1b6044820152606401610316565b60008080610d928161029a36879003870187611b51565b925092509250600081118015610da757504381105b610de35760405162461bcd60e51b815260206004820152600d60248201526c0756e66696c6c6564207377617609c1b6044820152606401610316565b610dee828535610c07565b15610e2e5760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b6044820152606401610316565b610e3d60a08501358535610c07565b610e7a5760405162461bcd60e51b815260206004820152600e60248201526d1a5b9d985b1a59081cd958dc995d60921b6044820152606401610316565b600083815260016020908152604090912060a08601359055610e9f9085013588611d0b565b9650505050508080610eb090611d1e565b915050610cd7565b5060006002600086866000818110610ed257610ed2611cdf565b905060c002016000016000013581526020019081526020016000205490506002600086866000818110610f0757610f07611cdf565b60c0029190910135825250602081019190915260400160009081208190556001600160a01b038316610f398386611e2a565b604051600081818185875af1925050503d8060008114610f75576040519150601f19603f3d011682016040523d82523d6000602084013e610f7a565b606091505b50909150506001811515146108e35760405162461bcd60e51b815260040161031690611de8565b600060028260000151836040015160601b846080015160601b856020015160001b866060015160c01b8860601b604051602001611022969594939291909586526bffffffffffffffffffffffff199485166020870152928416603486015260488501919091526001600160c01b031916606884015216607082015260840190565b60408051601f198184030181529082905261103c91611d7c565b602060405180830381855afa158015611059573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906105379190611e11565b6040805160608101825260008082526020820181905291810182905290806110ad8561029a36879003870187611b51565b92509250506110d76040805160608101909152806000815260006020820181905260409091015290565b816000036110fe578060005b908160038111156110f6576110f6611aa6565b9052506102bd565b6001830161110e578060036110e3565b611119838635610c07565b1561112e5760028152602081018390526102bd565b600181526040810191909152949350505050565b3233146111615760405162461bcd60e51b815260040161031690611d37565b6000805b8281101561131c573684848381811061118057611180611cdf565b60c00291909101915033905061119c60a0830160808401611dcd565b6001600160a01b0316146111df5760405162461bcd60e51b815260206004820152600a6024820152691b9bdd08185d5d1a195960b21b6044820152606401610316565b600080806111f68961029a36879003870187611b51565b92509250925060008111801561120b57504381105b6112475760405162461bcd60e51b815260206004820152600d60248201526c0756e66696c6c6564207377617609c1b6044820152606401610316565b611252828535610c07565b156112925760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b6044820152606401610316565b6112a160a08501358535610c07565b6112de5760405162461bcd60e51b815260206004820152600e60248201526d1a5b9d985b1a59081cd958dc995d60921b6044820152606401610316565b600083815260016020908152604090912060a086013590556113039085013587611d0b565b955050505050808061131490611d1e565b915050611165565b506001600160a01b03841661139f57604051600090339083908381818185875af1925050503d806000811461136d576040519150601f19603f3d011682016040523d82523d6000602084013e611372565b606091505b50909150506001811515146113995760405162461bcd60e51b815260040161031690611de8565b506108e6565b60408051336024820152604480820184905282518083039091018152606490910182526020810180516001600160e01b031663a9059cbb60e01b17905290516000916060916001600160a01b038816916113f891611d7c565b6000604051808303816000865af19150503d8060008114611435576040519150601f19603f3d011682016040523d82523d6000602084013e61143a565b606091505b5090925090508180156114655750805115806114655750808060200190518101906114659190611dab565b6108e35760405162461bcd60e51b815260040161031690611de8565b6000806000806114918686610fa1565b60008181526001602052604090205490979096508695509350505050565b60008060006114be8585611523565b915091506114cb81611568565b509392505050565b801561152057604051600090339060001990849084818181858888f193505050503d8060008114610c00576040519150601f19603f3d011682016040523d82523d6000602084013e610c00565b50565b60008082516041036115595760208301516040840151606085015160001a61154d878285856116b2565b94509450505050611561565b506000905060025b9250929050565b600081600481111561157c5761157c611aa6565b036115845750565b600181600481111561159857611598611aa6565b036115e55760405162461bcd60e51b815260206004820152601860248201527f45434453413a20696e76616c6964207369676e617475726500000000000000006044820152606401610316565b60028160048111156115f9576115f9611aa6565b036116465760405162461bcd60e51b815260206004820152601f60248201527f45434453413a20696e76616c6964207369676e6174757265206c656e677468006044820152606401610316565b600381600481111561165a5761165a611aa6565b036115205760405162461bcd60e51b815260206004820152602260248201527f45434453413a20696e76616c6964207369676e6174757265202773272076616c604482015261756560f01b6064820152608401610316565b6000807f7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a08311156116e9575060009050600361176d565b6040805160008082526020820180845289905260ff881692820192909252606081018690526080810185905260019060a0016020604051602081039080840390855afa15801561173d573d6000803e3d6000fd5b5050604051601f1901519150506001600160a01b0381166117665760006001925092505061176d565b9150600090505b94509492505050565b80356001600160a01b038116811461178d57600080fd5b919050565b60008082840360c08112156117a657600080fd5b6117af84611776565b925060a0601f19820112156117c357600080fd5b506020830190509250929050565b6000806000606084860312156117e657600080fd5b83356001600160401b038111156117fc57600080fd5b8401610160818703121561180f57600080fd5b95602085013595506040909401359392505050565b60008060006040848603121561183957600080fd5b61184284611776565b925060208401356001600160401b038082111561185e57600080fd5b818601915086601f83011261187257600080fd5b81358181111561188157600080fd5b87602060a08302850101111561189657600080fd5b6020830194508093505050509250925092565b600080604083850312156118bc57600080fd5b50508035926020909101359150565b60008083601f8401126118dd57600080fd5b5081356001600160401b038111156118f457600080fd5b60208301915083602060c08302850101111561156157600080fd5b6000806020838503121561192257600080fd5b82356001600160401b0381111561193857600080fd5b611944858286016118cb565b90969095509350505050565b634e487b7160e01b600052604160045260246000fd5b604080519081016001600160401b038111828210171561198857611988611950565b60405290565b604051601f8201601f191681016001600160401b03811182821017156119b6576119b6611950565b604052919050565b80356001600160401b038116811461178d57600080fd5b600060a082840312156119e757600080fd5b60405160a081018181106001600160401b0382111715611a0957611a09611950565b80604052508091508235815260208301356020820152611a2b60408401611776565b6040820152611a3c606084016119be565b6060820152611a4d60808401611776565b60808201525092915050565b60008060c08385031215611a6c57600080fd5b611a7583611776565b9150611a8484602085016119d5565b90509250929050565b600060208284031215611a9f57600080fd5b5035919050565b634e487b7160e01b600052602160045260246000fd5b8151606082019060048110611ae157634e487b7160e01b600052602160045260246000fd5b80835250602083015160208301526040830151604083015292915050565b600080600060408486031215611b1457600080fd5b611b1d84611776565b925060208401356001600160401b03811115611b3857600080fd5b611b44868287016118cb565b9497909650939450505050565b600060a08284031215611b6357600080fd5b61053783836119d5565b6000808335601e19843603018112611b8457600080fd5b8301803591506001600160401b03821115611b9e57600080fd5b60200191503681900382131561156157600080fd5b60008085851115611bc357600080fd5b83861115611bd057600080fd5b5050820193919092039150565b6001600160e01b03198135818116916004851015611c055780818660040360031b1b83161692505b505092915050565b60006020808385031215611c2057600080fd5b82356001600160401b0380821115611c3757600080fd5b818501915085601f830112611c4b57600080fd5b813581811115611c5d57611c5d611950565b611c6b848260051b0161198e565b818152848101925060c0918202840185019188831115611c8a57600080fd5b938501935b82851015611cd35780858a031215611ca75760008081fd5b611caf611966565b611cb98a876119d5565b815260a08601358782015284529384019392850192611c8f565b50979650505050505050565b634e487b7160e01b600052603260045260246000fd5b634e487b7160e01b600052601160045260246000fd5b808201808211156102c2576102c2611cf5565b600060018201611d3057611d30611cf5565b5060010190565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b600060208284031215611d7357600080fd5b610537826119be565b6000825160005b81811015611d9d5760208186018101518583015201611d83565b506000920191825250919050565b600060208284031215611dbd57600080fd5b8151801515811461053757600080fd5b600060208284031215611ddf57600080fd5b61053782611776565b6020808252600f908201526e1d1c985b9cd9995c8819985a5b1959608a1b604082015260600190565b600060208284031215611e2357600080fd5b5051919050565b818103818111156102c2576102c2611cf556fea2646970667358221220b79d8f6175c99dc48e9fe8bab17e68949db547d5e3aa2ff4653840c72a3b44a764736f6c63430008120033",
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
// Solidity: function status(address token, (bytes32,uint256,address,uint64,address) v) view returns((uint8,bytes32,uint256))
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
// Solidity: function status(address token, (bytes32,uint256,address,uint64,address) v) view returns((uint8,bytes32,uint256))
func (_ETHSwap *ETHSwapSession) Status(token common.Address, v ETHSwapVector) (ETHSwapStatus, error) {
	return _ETHSwap.Contract.Status(&_ETHSwap.CallOpts, token, v)
}

// Status is a free data retrieval call binding the contract method 0xf0e3b8d5.
//
// Solidity: function status(address token, (bytes32,uint256,address,uint64,address) v) view returns((uint8,bytes32,uint256))
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
// Solidity: function initiate(address token, (bytes32,uint256,address,uint64,address)[] contracts) payable returns()
func (_ETHSwap *ETHSwapTransactor) Initiate(opts *bind.TransactOpts, token common.Address, contracts []ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "initiate", token, contracts)
}

// Initiate is a paid mutator transaction binding the contract method 0x52145bc0.
//
// Solidity: function initiate(address token, (bytes32,uint256,address,uint64,address)[] contracts) payable returns()
func (_ETHSwap *ETHSwapSession) Initiate(token common.Address, contracts []ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.Contract.Initiate(&_ETHSwap.TransactOpts, token, contracts)
}

// Initiate is a paid mutator transaction binding the contract method 0x52145bc0.
//
// Solidity: function initiate(address token, (bytes32,uint256,address,uint64,address)[] contracts) payable returns()
func (_ETHSwap *ETHSwapTransactorSession) Initiate(token common.Address, contracts []ETHSwapVector) (*types.Transaction, error) {
	return _ETHSwap.Contract.Initiate(&_ETHSwap.TransactOpts, token, contracts)
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
// Solidity: function validateUserOp((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp, bytes32 userOpHash, uint256 missingAccountFunds) returns(uint256 validationData)
func (_ETHSwap *ETHSwapTransactor) ValidateUserOp(opts *bind.TransactOpts, userOp UserOperation, userOpHash [32]byte, missingAccountFunds *big.Int) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "validateUserOp", userOp, userOpHash, missingAccountFunds)
}

// ValidateUserOp is a paid mutator transaction binding the contract method 0x3a871cdd.
//
// Solidity: function validateUserOp((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp, bytes32 userOpHash, uint256 missingAccountFunds) returns(uint256 validationData)
func (_ETHSwap *ETHSwapSession) ValidateUserOp(userOp UserOperation, userOpHash [32]byte, missingAccountFunds *big.Int) (*types.Transaction, error) {
	return _ETHSwap.Contract.ValidateUserOp(&_ETHSwap.TransactOpts, userOp, userOpHash, missingAccountFunds)
}

// ValidateUserOp is a paid mutator transaction binding the contract method 0x3a871cdd.
//
// Solidity: function validateUserOp((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp, bytes32 userOpHash, uint256 missingAccountFunds) returns(uint256 validationData)
func (_ETHSwap *ETHSwapTransactorSession) ValidateUserOp(userOp UserOperation, userOpHash [32]byte, missingAccountFunds *big.Int) (*types.Transaction, error) {
	return _ETHSwap.Contract.ValidateUserOp(&_ETHSwap.TransactOpts, userOp, userOpHash, missingAccountFunds)
}
