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
	ABI: "[{\"inputs\":[{\"internalType\":\"addresspayable\",\"name\":\"_entryPoint\",\"type\":\"address\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"contractKey\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"entryPoint\",\"outputs\":[{\"internalType\":\"addresspayable\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector[]\",\"name\":\"contracts\",\"type\":\"tuple[]\"}],\"name\":\"initiate\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"isRedeemable\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeem\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"}],\"internalType\":\"structETHSwap.Redemption[]\",\"name\":\"redemptions\",\"type\":\"tuple[]\"}],\"name\":\"redeemAA\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"redeemPrepayments\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"refund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"}],\"name\":\"secretValidates\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"secretHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"initiator\",\"type\":\"address\"},{\"internalType\":\"uint64\",\"name\":\"refundTimestamp\",\"type\":\"uint64\"},{\"internalType\":\"address\",\"name\":\"participant\",\"type\":\"address\"}],\"internalType\":\"structETHSwap.Vector\",\"name\":\"v\",\"type\":\"tuple\"}],\"name\":\"status\",\"outputs\":[{\"components\":[{\"internalType\":\"enumETHSwap.Step\",\"name\":\"step\",\"type\":\"uint8\"},{\"internalType\":\"bytes32\",\"name\":\"secret\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"internalType\":\"structETHSwap.Status\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"swaps\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nonce\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"initCode\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"callData\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"callGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"verificationGasLimit\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"preVerificationGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"maxPriorityFeePerGas\",\"type\":\"uint256\"},{\"internalType\":\"bytes\",\"name\":\"paymasterAndData\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"signature\",\"type\":\"bytes\"}],\"internalType\":\"structUserOperation\",\"name\":\"userOp\",\"type\":\"tuple\"},{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"missingAccountFunds\",\"type\":\"uint256\"}],\"name\":\"validateUserOp\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"validationData\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561001057600080fd5b50604051611bb3380380611bb383398101604081905261002f91610054565b600080546001600160a01b0319166001600160a01b0392909216919091179055610084565b60006020828403121561006657600080fd5b81516001600160a01b038116811461007d57600080fd5b9392505050565b611b20806100936000396000f3fe6080604052600436106100a75760003560e01c80639ef07b4c116100645780639ef07b4c14610184578063b0d691fe146101a4578063eb84e7f2146101dc578063f0e3b8d514610209578063f521b2eb14610236578063f9f2e0f41461026357600080fd5b806333a3bcb4146100ac5780633a871cdd146100e157806352145bc01461010f57806371b6d0111461012457806377d7e03114610144578063919835a414610164575b600080fd5b3480156100b857600080fd5b506100cc6100c7366004611438565b610283565b60405190151581526020015b60405180910390f35b3480156100ed57600080fd5b506101016100fc366004611477565b6102c8565b6040519081526020016100d8565b61012261011d3660046114ca565b6104ce565b005b34801561013057600080fd5b5061012261013f366004611438565b61087c565b34801561015057600080fd5b506100cc61015f36600461154f565b610b59565b34801561017057600080fd5b5061012261017f3660046115bc565b610bd3565b34801561019057600080fd5b5061010161019f366004611706565b610ebe565b3480156101b057600080fd5b506000546101c4906001600160a01b031681565b6040516001600160a01b0390911681526020016100d8565b3480156101e857600080fd5b506101016101f736600461173a565b60016020526000908152604090205481565b34801561021557600080fd5b50610229610224366004611438565b610f99565b6040516100d89190611769565b34801561024257600080fd5b5061010161025136600461173a565b60026020526000908152604090205481565b34801561026f57600080fd5b5061012261027e3660046117ac565b61105f565b6000808061029f8561029a368790038701876117fe565b61139e565b9250925050806000141580156102bd57506102bb828535610b59565b155b925050505b92915050565b600080546001600160a01b0316331461031f5760405162461bcd60e51b81526020600482015260146024820152731cd95b99195c88084f48195b9d1c9e541bda5b9d60621b60448201526064015b60405180910390fd5b600461032e606086018661181a565b9050101561033e575060016104c7565b6324660d6960e21b610353606086018661181a565b61036291600491600091611860565b61036b9161188a565b6001600160e01b03191614610382575060026104c7565b6000610391606086018661181a565b61039f916004908290611860565b8101906103ac91906118ba565b905060008060005b83518110156104a15760008482815181106103d1576103d161198c565b602002602001015190506000806103ed6000846000015161139e565b9250925050806000148061040a575082515161040a908390610b59565b1561041f5760039750505050505050506104c7565b836000036104485782516080810151905160009081526002602052604090208a90559550610479565b856001600160a01b03168360000151608001516001600160a01b0316146104795760049750505050505050506104c7565b82516020015161048990866119b8565b94505050508080610499906119cb565b9150506103b4565b50808511156104b657600593505050506104c7565b6104bf856113cc565b600093505050505b9392505050565b3233146104ed5760405162461bcd60e51b8152600401610316906119e4565b6000805b82811015610717573684848381811061050c5761050c61198c565b905060a00201905060008160200135116105505760405162461bcd60e51b81526020600482015260056024820152640c081d985b60da1b6044820152606401610316565b60006105626080830160608401611a0e565b6001600160401b0316116105ac5760405162461bcd60e51b815260206004820152601160248201527003020726566756e6454696d657374616d7607c1b6044820152606401610316565b7f5069ec89f08d9ca0424bb5a5f59c3c60ed50cf06af5911a368e41e771763bfaf81350161062d5760405162461bcd60e51b815260206004820152602860248201527f696c6c6567616c2073656372657420686173682028726566756e64207265636f604482015267726420686173682960c01b6064820152608401610316565b60006106428761019f368590038501856117fe565b60008181526001602052604090205490915080156106935760405162461bcd60e51b815260206004820152600e60248201526d73776170206e6f7420656d70747960901b6044820152606401610316565b50436106a0818435610b59565b156106de5760405162461bcd60e51b815260206004820152600e60248201526d3430b9b41031b7b63634b9b4b7b760911b6044820152606401610316565b60008281526001602090815260409091208290556106ff90840135866119b8565b9450505050808061070f906119cb565b9150506104f1565b506001600160a01b0384166107645734811461075f5760405162461bcd60e51b8152602060048201526007602482015266189859081d985b60ca1b6044820152606401610316565b610876565b60408051336024820152306044820152606480820184905282518083039091018152608490910182526020810180516001600160e01b03166323b872dd60e01b17905290516000916060916001600160a01b038816916107c391611a29565b6000604051808303816000865af19150503d8060008114610800576040519150601f19603f3d011682016040523d82523d6000602084013e610805565b606091505b5090925090508180156108305750805115806108305750808060200190518101906108309190611a58565b6108735760405162461bcd60e51b81526020600482015260146024820152731d1c985b9cd9995c88199c9bdb4819985a5b195960621b6044820152606401610316565b50505b50505050565b32331461089b5760405162461bcd60e51b8152600401610316906119e4565b6108ab6080820160608301611a0e565b6001600160401b03164210156108fa5760405162461bcd60e51b81526020600482015260146024820152731b1bd8dadd1a5b59481b9bdd08195e1c1a5c995960621b6044820152606401610316565b600080806109118561029a368790038701876117fe565b9250925092506000811180156109275750438111155b6109655760405162461bcd60e51b815260206004820152600f60248201526e73776170206e6f742061637469766560881b6044820152606401610316565b610970828535610b59565b156109b55760405162461bcd60e51b81526020600482015260156024820152741cddd85c08185b1c9958591e481c995919595b5959605a1b6044820152606401610316565b600083815260016020526040902060001990556001600160a01b038516610a685760006109e86060860160408701611a7a565b6001600160a01b0316856020013560405160006040518083038185875af1925050503d8060008114610a36576040519150601f19603f3d011682016040523d82523d6000602084013e610a3b565b606091505b5090915050600181151514610a625760405162461bcd60e51b815260040161031690611a95565b50610b52565b604080513360248201526020868101356044808401919091528351808403909101815260649092018352810180516001600160e01b031663a9059cbb60e01b17905290516000916060916001600160a01b03891691610ac691611a29565b6000604051808303816000865af19150503d8060008114610b03576040519150601f19603f3d011682016040523d82523d6000602084013e610b08565b606091505b509092509050818015610b33575080511580610b33575080806020019051810190610b339190611a58565b610b4f5760405162461bcd60e51b815260040161031690611a95565b50505b5050505050565b600081600284604051602001610b7191815260200190565b60408051601f1981840301815290829052610b8b91611a29565b602060405180830381855afa158015610ba8573d6000803e3d6000fd5b5050506040513d601f19601f82011682018060405250810190610bcb9190611abe565b149392505050565b6000546001600160a01b03163314610c245760405162461bcd60e51b81526020600482015260146024820152731cd95b99195c88084f48195b9d1c9e541bda5b9d60621b6044820152606401610316565b600080805b83811015610e0a5736858583818110610c4457610c4461198c565b905060c00201905081600003610c6b57610c6460a0820160808301611a7a565b9250610ccd565b6001600160a01b038316610c8560a0830160808401611a7a565b6001600160a01b031614610ccd5760405162461bcd60e51b815260206004820152600f60248201526e189859081c185c9d1a58da5c185b9d608a1b6044820152606401610316565b60008080610ce48161029a368790038701876117fe565b925092509250600081118015610cf957504381105b610d355760405162461bcd60e51b815260206004820152600d60248201526c0756e66696c6c6564207377617609c1b6044820152606401610316565b610d40828535610b59565b15610d805760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b6044820152606401610316565b610d8f60a08501358535610b59565b610dcc5760405162461bcd60e51b815260206004820152600e60248201526d1a5b9d985b1a59081cd958dc995d60921b6044820152606401610316565b600083815260016020908152604090912060a08601359055610df190850135886119b8565b9650505050508080610e02906119cb565b915050610c29565b5060006002600086866000818110610e2457610e2461198c565b60c0029190910135825250602081019190915260400160009081205491506001600160a01b038316610e568386611ad7565b604051600081818185875af1925050503d8060008114610e92576040519150601f19603f3d011682016040523d82523d6000602084013e610e97565b606091505b50909150506001811515146108735760405162461bcd60e51b815260040161031690611a95565b600060028260000151836040015160601b846080015160601b856020015160001b866060015160c01b8860601b604051602001610f3f969594939291909586526bffffffffffffffffffffffff199485166020870152928416603486015260488501919091526001600160c01b031916606884015216607082015260840190565b60408051601f1981840301815290829052610f5991611a29565b602060405180830381855afa158015610f76573d6000803e3d6000fd5b5050506040513d601f19601f820116820180604052508101906104c79190611abe565b604080516060810182526000808252602082018190529181018290529080610fca8561029a368790038701876117fe565b9250925050610ff46040805160608101909152806000815260006020820181905260409091015290565b8160000361101b578060005b9081600381111561101357611013611753565b9052506102bd565b6001830161102b57806003611000565b611036838635610b59565b1561104b5760028152602081018390526102bd565b600181526040810191909152949350505050565b32331461107e5760405162461bcd60e51b8152600401610316906119e4565b6000805b82811015611239573684848381811061109d5761109d61198c565b60c0029190910191503390506110b960a0830160808401611a7a565b6001600160a01b0316146110fc5760405162461bcd60e51b815260206004820152600a6024820152691b9bdd08185d5d1a195960b21b6044820152606401610316565b600080806111138961029a368790038701876117fe565b92509250925060008111801561112857504381105b6111645760405162461bcd60e51b815260206004820152600d60248201526c0756e66696c6c6564207377617609c1b6044820152606401610316565b61116f828535610b59565b156111af5760405162461bcd60e51b815260206004820152601060248201526f185b1c9958591e481c995919595b595960821b6044820152606401610316565b6111be60a08501358535610b59565b6111fb5760405162461bcd60e51b815260206004820152600e60248201526d1a5b9d985b1a59081cd958dc995d60921b6044820152606401610316565b600083815260016020908152604090912060a0860135905561122090850135876119b8565b9550505050508080611231906119cb565b915050611082565b506001600160a01b0384166112bc57604051600090339083908381818185875af1925050503d806000811461128a576040519150601f19603f3d011682016040523d82523d6000602084013e61128f565b606091505b50909150506001811515146112b65760405162461bcd60e51b815260040161031690611a95565b50610876565b60408051336024820152604480820184905282518083039091018152606490910182526020810180516001600160e01b031663a9059cbb60e01b17905290516000916060916001600160a01b0388169161131591611a29565b6000604051808303816000865af19150503d8060008114611352576040519150601f19603f3d011682016040523d82523d6000602084013e611357565b606091505b5090925090508180156113825750805115806113825750808060200190518101906113829190611a58565b6108735760405162461bcd60e51b815260040161031690611a95565b6000806000806113ae8686610ebe565b60008181526001602052604090205490979096508695509350505050565b801561141957604051600090339060001990849084818181858888f193505050503d8060008114610b52576040519150601f19603f3d011682016040523d82523d6000602084013e610b52565b50565b80356001600160a01b038116811461143357600080fd5b919050565b60008082840360c081121561144c57600080fd5b6114558461141c565b925060a0601f198201121561146957600080fd5b506020830190509250929050565b60008060006060848603121561148c57600080fd5b83356001600160401b038111156114a257600080fd5b840161016081870312156114b557600080fd5b95602085013595506040909401359392505050565b6000806000604084860312156114df57600080fd5b6114e88461141c565b925060208401356001600160401b038082111561150457600080fd5b818601915086601f83011261151857600080fd5b81358181111561152757600080fd5b87602060a08302850101111561153c57600080fd5b6020830194508093505050509250925092565b6000806040838503121561156257600080fd5b50508035926020909101359150565b60008083601f84011261158357600080fd5b5081356001600160401b0381111561159a57600080fd5b60208301915083602060c0830285010111156115b557600080fd5b9250929050565b600080602083850312156115cf57600080fd5b82356001600160401b038111156115e557600080fd5b6115f185828601611571565b90969095509350505050565b634e487b7160e01b600052604160045260246000fd5b604080519081016001600160401b0381118282101715611635576116356115fd565b60405290565b604051601f8201601f191681016001600160401b0381118282101715611663576116636115fd565b604052919050565b80356001600160401b038116811461143357600080fd5b600060a0828403121561169457600080fd5b60405160a081018181106001600160401b03821117156116b6576116b66115fd565b806040525080915082358152602083013560208201526116d86040840161141c565b60408201526116e96060840161166b565b60608201526116fa6080840161141c565b60808201525092915050565b60008060c0838503121561171957600080fd5b6117228361141c565b91506117318460208501611682565b90509250929050565b60006020828403121561174c57600080fd5b5035919050565b634e487b7160e01b600052602160045260246000fd5b815160608201906004811061178e57634e487b7160e01b600052602160045260246000fd5b80835250602083015160208301526040830151604083015292915050565b6000806000604084860312156117c157600080fd5b6117ca8461141c565b925060208401356001600160401b038111156117e557600080fd5b6117f186828701611571565b9497909650939450505050565b600060a0828403121561181057600080fd5b6104c78383611682565b6000808335601e1984360301811261183157600080fd5b8301803591506001600160401b0382111561184b57600080fd5b6020019150368190038213156115b557600080fd5b6000808585111561187057600080fd5b8386111561187d57600080fd5b5050820193919092039150565b6001600160e01b031981358181169160048510156118b25780818660040360031b1b83161692505b505092915050565b600060208083850312156118cd57600080fd5b82356001600160401b03808211156118e457600080fd5b818501915085601f8301126118f857600080fd5b81358181111561190a5761190a6115fd565b611918848260051b0161163b565b818152848101925060c091820284018501918883111561193757600080fd5b938501935b828510156119805780858a0312156119545760008081fd5b61195c611613565b6119668a87611682565b815260a0860135878201528452938401939285019261193c565b50979650505050505050565b634e487b7160e01b600052603260045260246000fd5b634e487b7160e01b600052601160045260246000fd5b808201808211156102c2576102c26119a2565b6000600182016119dd576119dd6119a2565b5060010190565b60208082526010908201526f39b2b73232b910109e9037b934b3b4b760811b604082015260600190565b600060208284031215611a2057600080fd5b6104c78261166b565b6000825160005b81811015611a4a5760208186018101518583015201611a30565b506000920191825250919050565b600060208284031215611a6a57600080fd5b815180151581146104c757600080fd5b600060208284031215611a8c57600080fd5b6104c78261141c565b6020808252600f908201526e1d1c985b9cd9995c8819985a5b1959608a1b604082015260600190565b600060208284031215611ad057600080fd5b5051919050565b818103818111156102c2576102c26119a256fea2646970667358221220e4700ac6f1bdc8af4d00d6c5cc6379f7cb53a054d1b73102fc7ee03e5db2a93d64736f6c63430008120033",
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
// Solidity: function validateUserOp((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp, bytes32 , uint256 missingAccountFunds) returns(uint256 validationData)
func (_ETHSwap *ETHSwapTransactor) ValidateUserOp(opts *bind.TransactOpts, userOp UserOperation, arg1 [32]byte, missingAccountFunds *big.Int) (*types.Transaction, error) {
	return _ETHSwap.contract.Transact(opts, "validateUserOp", userOp, arg1, missingAccountFunds)
}

// ValidateUserOp is a paid mutator transaction binding the contract method 0x3a871cdd.
//
// Solidity: function validateUserOp((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp, bytes32 , uint256 missingAccountFunds) returns(uint256 validationData)
func (_ETHSwap *ETHSwapSession) ValidateUserOp(userOp UserOperation, arg1 [32]byte, missingAccountFunds *big.Int) (*types.Transaction, error) {
	return _ETHSwap.Contract.ValidateUserOp(&_ETHSwap.TransactOpts, userOp, arg1, missingAccountFunds)
}

// ValidateUserOp is a paid mutator transaction binding the contract method 0x3a871cdd.
//
// Solidity: function validateUserOp((address,uint256,bytes,bytes,uint256,uint256,uint256,uint256,uint256,bytes,bytes) userOp, bytes32 , uint256 missingAccountFunds) returns(uint256 validationData)
func (_ETHSwap *ETHSwapTransactorSession) ValidateUserOp(userOp UserOperation, arg1 [32]byte, missingAccountFunds *big.Int) (*types.Transaction, error) {
	return _ETHSwap.Contract.ValidateUserOp(&_ETHSwap.TransactOpts, userOp, arg1, missingAccountFunds)
}
