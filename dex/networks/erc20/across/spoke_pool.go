// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package across

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

// MultiCallerUpgradeableResult is an auto generated low-level Go binding around an user-defined struct.
type MultiCallerUpgradeableResult struct {
	Success    bool
	ReturnData []byte
}

// SpokePoolInterfaceRelayerRefundLeaf is an auto generated low-level Go binding around an user-defined struct.
type SpokePoolInterfaceRelayerRefundLeaf struct {
	AmountToReturn  *big.Int
	ChainId         *big.Int
	RefundAmounts   []*big.Int
	LeafId          uint32
	L2TokenAddress  common.Address
	RefundAddresses []common.Address
}

// V3SpokePoolInterfaceLegacyV3RelayExecutionEventInfo is an auto generated low-level Go binding around an user-defined struct.
type V3SpokePoolInterfaceLegacyV3RelayExecutionEventInfo struct {
	UpdatedRecipient    common.Address
	UpdatedMessage      []byte
	UpdatedOutputAmount *big.Int
	FillType            uint8
}

// V3SpokePoolInterfaceV3RelayData is an auto generated low-level Go binding around an user-defined struct.
type V3SpokePoolInterfaceV3RelayData struct {
	Depositor           [32]byte
	Recipient           [32]byte
	ExclusiveRelayer    [32]byte
	InputToken          [32]byte
	OutputToken         [32]byte
	InputAmount         *big.Int
	OutputAmount        *big.Int
	OriginChainId       *big.Int
	DepositId           *big.Int
	FillDeadline        uint32
	ExclusivityDeadline uint32
	Message             []byte
}

// V3SpokePoolInterfaceV3RelayDataLegacy is an auto generated low-level Go binding around an user-defined struct.
type V3SpokePoolInterfaceV3RelayDataLegacy struct {
	Depositor           common.Address
	Recipient           common.Address
	ExclusiveRelayer    common.Address
	InputToken          common.Address
	OutputToken         common.Address
	InputAmount         *big.Int
	OutputAmount        *big.Int
	OriginChainId       *big.Int
	DepositId           uint32
	FillDeadline        uint32
	ExclusivityDeadline uint32
	Message             []byte
}

// V3SpokePoolInterfaceV3RelayExecutionEventInfo is an auto generated low-level Go binding around an user-defined struct.
type V3SpokePoolInterfaceV3RelayExecutionEventInfo struct {
	UpdatedRecipient    [32]byte
	UpdatedMessageHash  [32]byte
	UpdatedOutputAmount *big.Int
	FillType            uint8
}

// V3SpokePoolInterfaceV3SlowFill is an auto generated low-level Go binding around an user-defined struct.
type V3SpokePoolInterfaceV3SlowFill struct {
	RelayData           V3SpokePoolInterfaceV3RelayData
	ChainId             *big.Int
	UpdatedOutputAmount *big.Int
}

// SpokePoolMetaData contains all meta data concerning the SpokePool contract.
var SpokePoolMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_wrappedNativeTokenAddress\",\"type\":\"address\"},{\"internalType\":\"uint32\",\"name\":\"_depositQuoteTimeBuffer\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"_fillDeadlineBuffer\",\"type\":\"uint32\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"ClaimedMerkleLeaf\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"DepositsArePaused\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"DisabledRoute\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ExpiredFillDeadline\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"FillsArePaused\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InsufficientSpokePoolBalanceToExecuteLeaf\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidBytes32\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidChainId\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidCrossDomainAdmin\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidDepositorSignature\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidExclusiveRelayer\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidFillDeadline\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidMerkleLeaf\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidMerkleProof\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidPayoutAdjustmentPct\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidQuoteTimestamp\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidRelayerFeePct\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidSlowFillRequest\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidWithdrawalRecipient\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"LowLevelCallFailed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"MaxTransferSizeExceeded\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"MsgValueDoesNotMatchInputAmount\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NoRelayerRefundToClaim\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NoSlowFillsInExclusivityWindow\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NotEOA\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NotExclusiveRelayer\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"RelayFilled\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"WrongERC7683OrderId\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"previousAdmin\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"newAdmin\",\"type\":\"address\"}],\"name\":\"AdminChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"beacon\",\"type\":\"address\"}],\"name\":\"BeaconUpgraded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"l2TokenAddress\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"refundAddress\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"caller\",\"type\":\"address\"}],\"name\":\"ClaimedRelayerRefund\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"rootBundleId\",\"type\":\"uint256\"}],\"name\":\"EmergencyDeletedRootBundle\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"originToken\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"destinationChainId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"enabled\",\"type\":\"bool\"}],\"name\":\"EnabledDepositRoute\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amountToReturn\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"chainId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256[]\",\"name\":\"refundAmounts\",\"type\":\"uint256[]\"},{\"indexed\":true,\"internalType\":\"uint32\",\"name\":\"rootBundleId\",\"type\":\"uint32\"},{\"indexed\":true,\"internalType\":\"uint32\",\"name\":\"leafId\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"l2TokenAddress\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address[]\",\"name\":\"refundAddresses\",\"type\":\"address[]\"},{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"deferredRefunds\",\"type\":\"bool\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"caller\",\"type\":\"address\"}],\"name\":\"ExecutedRelayerRefundRoot\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"inputToken\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"outputToken\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"repaymentChainId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"originChainId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"depositId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"exclusivityDeadline\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"exclusiveRelayer\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"relayer\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"recipient\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"messageHash\",\"type\":\"bytes32\"},{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"updatedRecipient\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"updatedMessageHash\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"updatedOutputAmount\",\"type\":\"uint256\"},{\"internalType\":\"enumV3SpokePoolInterface.FillType\",\"name\":\"fillType\",\"type\":\"uint8\"}],\"indexed\":false,\"internalType\":\"structV3SpokePoolInterface.V3RelayExecutionEventInfo\",\"name\":\"relayExecutionInfo\",\"type\":\"tuple\"}],\"name\":\"FilledRelay\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"inputToken\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"outputToken\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"repaymentChainId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"originChainId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint32\",\"name\":\"depositId\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"exclusivityDeadline\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"exclusiveRelayer\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"relayer\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"},{\"components\":[{\"internalType\":\"address\",\"name\":\"updatedRecipient\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"updatedMessage\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"updatedOutputAmount\",\"type\":\"uint256\"},{\"internalType\":\"enumV3SpokePoolInterface.FillType\",\"name\":\"fillType\",\"type\":\"uint8\"}],\"indexed\":false,\"internalType\":\"structV3SpokePoolInterface.LegacyV3RelayExecutionEventInfo\",\"name\":\"relayExecutionInfo\",\"type\":\"tuple\"}],\"name\":\"FilledV3Relay\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"inputToken\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"outputToken\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"destinationChainId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"depositId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"quoteTimestamp\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"exclusivityDeadline\",\"type\":\"uint32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"recipient\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"exclusiveRelayer\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"name\":\"FundsDeposited\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint8\",\"name\":\"version\",\"type\":\"uint8\"}],\"name\":\"Initialized\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"previousOwner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"OwnershipTransferred\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"isPaused\",\"type\":\"bool\"}],\"name\":\"PausedDeposits\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"isPaused\",\"type\":\"bool\"}],\"name\":\"PausedFills\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint32\",\"name\":\"rootBundleId\",\"type\":\"uint32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"relayerRefundRoot\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"slowRelayRoot\",\"type\":\"bytes32\"}],\"name\":\"RelayedRootBundle\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"inputToken\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"outputToken\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"originChainId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"depositId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"exclusivityDeadline\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"exclusiveRelayer\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"recipient\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"messageHash\",\"type\":\"bytes32\"}],\"name\":\"RequestedSlowFill\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"updatedOutputAmount\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"depositId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes32\",\"name\":\"updatedRecipient\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"updatedMessage\",\"type\":\"bytes\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"depositorSignature\",\"type\":\"bytes\"}],\"name\":\"RequestedSpeedUpDeposit\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"updatedOutputAmount\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint32\",\"name\":\"depositId\",\"type\":\"uint32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"updatedRecipient\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"updatedMessage\",\"type\":\"bytes\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"depositorSignature\",\"type\":\"bytes\"}],\"name\":\"RequestedSpeedUpV3Deposit\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"inputToken\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"outputToken\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"originChainId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint32\",\"name\":\"depositId\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"exclusivityDeadline\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"exclusiveRelayer\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"name\":\"RequestedV3SlowFill\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newWithdrawalRecipient\",\"type\":\"address\"}],\"name\":\"SetWithdrawalRecipient\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"newAdmin\",\"type\":\"address\"}],\"name\":\"SetXDomainAdmin\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amountToReturn\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"chainId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint32\",\"name\":\"leafId\",\"type\":\"uint32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"l2TokenAddress\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"caller\",\"type\":\"address\"}],\"name\":\"TokensBridged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"implementation\",\"type\":\"address\"}],\"name\":\"Upgraded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"address\",\"name\":\"inputToken\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"outputToken\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"destinationChainId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"uint32\",\"name\":\"depositId\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"quoteTimestamp\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"indexed\":false,\"internalType\":\"uint32\",\"name\":\"exclusivityDeadline\",\"type\":\"uint32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"exclusiveRelayer\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"name\":\"V3FundsDeposited\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"EMPTY_RELAYER\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"EMPTY_REPAYMENT_CHAIN_ID\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"INFINITE_FILL_DEADLINE\",\"outputs\":[{\"internalType\":\"uint32\",\"name\":\"\",\"type\":\"uint32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"MAX_EXCLUSIVITY_PERIOD_SECONDS\",\"outputs\":[{\"internalType\":\"uint32\",\"name\":\"\",\"type\":\"uint32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"MAX_TRANSFER_SIZE\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"UPDATE_BYTES32_DEPOSIT_DETAILS_HASH\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"_initialDepositId\",\"type\":\"uint32\"},{\"internalType\":\"address\",\"name\":\"_crossDomainAdmin\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_withdrawalRecipient\",\"type\":\"address\"}],\"name\":\"__SpokePool_init\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"chainId\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"l2TokenAddress\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"refundAddress\",\"type\":\"bytes32\"}],\"name\":\"claimRelayerRefund\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"crossDomainAdmin\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"recipient\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"inputToken\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"outputToken\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"destinationChainId\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"exclusiveRelayer\",\"type\":\"bytes32\"},{\"internalType\":\"uint32\",\"name\":\"quoteTimestamp\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"exclusivityParameter\",\"type\":\"uint32\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"name\":\"deposit\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"originToken\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"destinationChainId\",\"type\":\"uint256\"},{\"internalType\":\"int64\",\"name\":\"relayerFeePct\",\"type\":\"int64\"},{\"internalType\":\"uint32\",\"name\":\"quoteTimestamp\",\"type\":\"uint32\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"depositDeprecated_5947912356\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"originToken\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"destinationChainId\",\"type\":\"uint256\"},{\"internalType\":\"int64\",\"name\":\"relayerFeePct\",\"type\":\"int64\"},{\"internalType\":\"uint32\",\"name\":\"quoteTimestamp\",\"type\":\"uint32\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"},{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"depositFor\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"recipient\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"inputToken\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"outputToken\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"destinationChainId\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"exclusiveRelayer\",\"type\":\"bytes32\"},{\"internalType\":\"uint32\",\"name\":\"fillDeadlineOffset\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"exclusivityParameter\",\"type\":\"uint32\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"name\":\"depositNow\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"depositQuoteTimeBuffer\",\"outputs\":[{\"internalType\":\"uint32\",\"name\":\"\",\"type\":\"uint32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"inputToken\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"outputToken\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"destinationChainId\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"exclusiveRelayer\",\"type\":\"address\"},{\"internalType\":\"uint32\",\"name\":\"quoteTimestamp\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"exclusivityParameter\",\"type\":\"uint32\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"name\":\"depositV3\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"inputToken\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"outputToken\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"destinationChainId\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"exclusiveRelayer\",\"type\":\"address\"},{\"internalType\":\"uint32\",\"name\":\"fillDeadlineOffset\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"exclusivityParameter\",\"type\":\"uint32\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"name\":\"depositV3Now\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"rootBundleId\",\"type\":\"uint256\"}],\"name\":\"emergencyDeleteRootBundle\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"rootBundleId\",\"type\":\"uint32\"},{\"components\":[{\"internalType\":\"uint256\",\"name\":\"amountToReturn\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"chainId\",\"type\":\"uint256\"},{\"internalType\":\"uint256[]\",\"name\":\"refundAmounts\",\"type\":\"uint256[]\"},{\"internalType\":\"uint32\",\"name\":\"leafId\",\"type\":\"uint32\"},{\"internalType\":\"address\",\"name\":\"l2TokenAddress\",\"type\":\"address\"},{\"internalType\":\"address[]\",\"name\":\"refundAddresses\",\"type\":\"address[]\"}],\"internalType\":\"structSpokePoolInterface.RelayerRefundLeaf\",\"name\":\"relayerRefundLeaf\",\"type\":\"tuple\"},{\"internalType\":\"bytes32[]\",\"name\":\"proof\",\"type\":\"bytes32[]\"}],\"name\":\"executeRelayerRefundLeaf\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"recipient\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"exclusiveRelayer\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"inputToken\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"outputToken\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"originChainId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"depositId\",\"type\":\"uint256\"},{\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"exclusivityDeadline\",\"type\":\"uint32\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"internalType\":\"structV3SpokePoolInterface.V3RelayData\",\"name\":\"relayData\",\"type\":\"tuple\"},{\"internalType\":\"uint256\",\"name\":\"chainId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"updatedOutputAmount\",\"type\":\"uint256\"}],\"internalType\":\"structV3SpokePoolInterface.V3SlowFill\",\"name\":\"slowFillLeaf\",\"type\":\"tuple\"},{\"internalType\":\"uint32\",\"name\":\"rootBundleId\",\"type\":\"uint32\"},{\"internalType\":\"bytes32[]\",\"name\":\"proof\",\"type\":\"bytes32[]\"}],\"name\":\"executeSlowRelayLeaf\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"orderId\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"originData\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"fillerData\",\"type\":\"bytes\"}],\"name\":\"fill\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"fillDeadlineBuffer\",\"outputs\":[{\"internalType\":\"uint32\",\"name\":\"\",\"type\":\"uint32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"recipient\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"exclusiveRelayer\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"inputToken\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"outputToken\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"originChainId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"depositId\",\"type\":\"uint256\"},{\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"exclusivityDeadline\",\"type\":\"uint32\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"internalType\":\"structV3SpokePoolInterface.V3RelayData\",\"name\":\"relayData\",\"type\":\"tuple\"},{\"internalType\":\"uint256\",\"name\":\"repaymentChainId\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"repaymentAddress\",\"type\":\"bytes32\"}],\"name\":\"fillRelay\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"recipient\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"exclusiveRelayer\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"inputToken\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"outputToken\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"originChainId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"depositId\",\"type\":\"uint256\"},{\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"exclusivityDeadline\",\"type\":\"uint32\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"internalType\":\"structV3SpokePoolInterface.V3RelayData\",\"name\":\"relayData\",\"type\":\"tuple\"},{\"internalType\":\"uint256\",\"name\":\"repaymentChainId\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"repaymentAddress\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"updatedOutputAmount\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"updatedRecipient\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"updatedMessage\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"depositorSignature\",\"type\":\"bytes\"}],\"name\":\"fillRelayWithUpdatedDeposit\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"name\":\"fillStatuses\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"exclusiveRelayer\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"inputToken\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"outputToken\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"originChainId\",\"type\":\"uint256\"},{\"internalType\":\"uint32\",\"name\":\"depositId\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"exclusivityDeadline\",\"type\":\"uint32\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"internalType\":\"structV3SpokePoolInterface.V3RelayDataLegacy\",\"name\":\"relayData\",\"type\":\"tuple\"},{\"internalType\":\"uint256\",\"name\":\"repaymentChainId\",\"type\":\"uint256\"}],\"name\":\"fillV3Relay\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getCurrentTime\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"l2TokenAddress\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"refundAddress\",\"type\":\"address\"}],\"name\":\"getRelayerRefund\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"msgSender\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"depositNonce\",\"type\":\"uint256\"}],\"name\":\"getUnsafeDepositId\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"pure\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"recipient\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"exclusiveRelayer\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"inputToken\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"outputToken\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"originChainId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"depositId\",\"type\":\"uint256\"},{\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"exclusivityDeadline\",\"type\":\"uint32\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"internalType\":\"structV3SpokePoolInterface.V3RelayData\",\"name\":\"relayData\",\"type\":\"tuple\"}],\"name\":\"getV3RelayHash\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint32\",\"name\":\"_initialDepositId\",\"type\":\"uint32\"},{\"internalType\":\"address\",\"name\":\"_withdrawalRecipient\",\"type\":\"address\"}],\"name\":\"initialize\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes[]\",\"name\":\"data\",\"type\":\"bytes[]\"}],\"name\":\"multicall\",\"outputs\":[{\"internalType\":\"bytes[]\",\"name\":\"results\",\"type\":\"bytes[]\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"numberOfDeposits\",\"outputs\":[{\"internalType\":\"uint32\",\"name\":\"\",\"type\":\"uint32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bool\",\"name\":\"pause\",\"type\":\"bool\"}],\"name\":\"pauseDeposits\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bool\",\"name\":\"pause\",\"type\":\"bool\"}],\"name\":\"pauseFills\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"pausedDeposits\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"pausedFills\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"proxiableUUID\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"relayerRefundRoot\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"slowRelayRoot\",\"type\":\"bytes32\"}],\"name\":\"relayRootBundle\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"name\":\"relayerRefund\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"renounceOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"components\":[{\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"recipient\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"exclusiveRelayer\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"inputToken\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"outputToken\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"originChainId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"depositId\",\"type\":\"uint256\"},{\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"exclusivityDeadline\",\"type\":\"uint32\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"internalType\":\"structV3SpokePoolInterface.V3RelayData\",\"name\":\"relayData\",\"type\":\"tuple\"}],\"name\":\"requestSlowFill\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"name\":\"rootBundles\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"slowRelayRoot\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"relayerRefundRoot\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newCrossDomainAdmin\",\"type\":\"address\"}],\"name\":\"setCrossDomainAdmin\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newWithdrawalRecipient\",\"type\":\"address\"}],\"name\":\"setWithdrawalRecipient\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"depositId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"updatedOutputAmount\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"updatedRecipient\",\"type\":\"bytes32\"},{\"internalType\":\"bytes\",\"name\":\"updatedMessage\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"depositorSignature\",\"type\":\"bytes\"}],\"name\":\"speedUpDeposit\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"depositor\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"depositId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"updatedOutputAmount\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"updatedRecipient\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"updatedMessage\",\"type\":\"bytes\"},{\"internalType\":\"bytes\",\"name\":\"depositorSignature\",\"type\":\"bytes\"}],\"name\":\"speedUpV3Deposit\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes[]\",\"name\":\"data\",\"type\":\"bytes[]\"}],\"name\":\"tryMulticall\",\"outputs\":[{\"components\":[{\"internalType\":\"bool\",\"name\":\"success\",\"type\":\"bool\"},{\"internalType\":\"bytes\",\"name\":\"returnData\",\"type\":\"bytes\"}],\"internalType\":\"structMultiCallerUpgradeable.Result[]\",\"name\":\"results\",\"type\":\"tuple[]\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"depositor\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"recipient\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"inputToken\",\"type\":\"bytes32\"},{\"internalType\":\"bytes32\",\"name\":\"outputToken\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"inputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"outputAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"destinationChainId\",\"type\":\"uint256\"},{\"internalType\":\"bytes32\",\"name\":\"exclusiveRelayer\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"depositNonce\",\"type\":\"uint256\"},{\"internalType\":\"uint32\",\"name\":\"quoteTimestamp\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"fillDeadline\",\"type\":\"uint32\"},{\"internalType\":\"uint32\",\"name\":\"exclusivityParameter\",\"type\":\"uint32\"},{\"internalType\":\"bytes\",\"name\":\"message\",\"type\":\"bytes\"}],\"name\":\"unsafeDeposit\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newImplementation\",\"type\":\"address\"}],\"name\":\"upgradeTo\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newImplementation\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"upgradeToAndCall\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"withdrawalRecipient\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"wrappedNativeToken\",\"outputs\":[{\"internalType\":\"contractWETH9Interface\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"stateMutability\":\"payable\",\"type\":\"receive\"}]",
}

// SpokePoolABI is the input ABI used to generate the binding from.
// Deprecated: Use SpokePoolMetaData.ABI instead.
var SpokePoolABI = SpokePoolMetaData.ABI

// SpokePool is an auto generated Go binding around an Ethereum contract.
type SpokePool struct {
	SpokePoolCaller     // Read-only binding to the contract
	SpokePoolTransactor // Write-only binding to the contract
	SpokePoolFilterer   // Log filterer for contract events
}

// SpokePoolCaller is an auto generated read-only Go binding around an Ethereum contract.
type SpokePoolCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SpokePoolTransactor is an auto generated write-only Go binding around an Ethereum contract.
type SpokePoolTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SpokePoolFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type SpokePoolFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SpokePoolSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type SpokePoolSession struct {
	Contract     *SpokePool        // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// SpokePoolCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type SpokePoolCallerSession struct {
	Contract *SpokePoolCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts    // Call options to use throughout this session
}

// SpokePoolTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type SpokePoolTransactorSession struct {
	Contract     *SpokePoolTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts    // Transaction auth options to use throughout this session
}

// SpokePoolRaw is an auto generated low-level Go binding around an Ethereum contract.
type SpokePoolRaw struct {
	Contract *SpokePool // Generic contract binding to access the raw methods on
}

// SpokePoolCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type SpokePoolCallerRaw struct {
	Contract *SpokePoolCaller // Generic read-only contract binding to access the raw methods on
}

// SpokePoolTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type SpokePoolTransactorRaw struct {
	Contract *SpokePoolTransactor // Generic write-only contract binding to access the raw methods on
}

// NewSpokePool creates a new instance of SpokePool, bound to a specific deployed contract.
func NewSpokePool(address common.Address, backend bind.ContractBackend) (*SpokePool, error) {
	contract, err := bindSpokePool(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &SpokePool{SpokePoolCaller: SpokePoolCaller{contract: contract}, SpokePoolTransactor: SpokePoolTransactor{contract: contract}, SpokePoolFilterer: SpokePoolFilterer{contract: contract}}, nil
}

// NewSpokePoolCaller creates a new read-only instance of SpokePool, bound to a specific deployed contract.
func NewSpokePoolCaller(address common.Address, caller bind.ContractCaller) (*SpokePoolCaller, error) {
	contract, err := bindSpokePool(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &SpokePoolCaller{contract: contract}, nil
}

// NewSpokePoolTransactor creates a new write-only instance of SpokePool, bound to a specific deployed contract.
func NewSpokePoolTransactor(address common.Address, transactor bind.ContractTransactor) (*SpokePoolTransactor, error) {
	contract, err := bindSpokePool(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &SpokePoolTransactor{contract: contract}, nil
}

// NewSpokePoolFilterer creates a new log filterer instance of SpokePool, bound to a specific deployed contract.
func NewSpokePoolFilterer(address common.Address, filterer bind.ContractFilterer) (*SpokePoolFilterer, error) {
	contract, err := bindSpokePool(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &SpokePoolFilterer{contract: contract}, nil
}

// bindSpokePool binds a generic wrapper to an already deployed contract.
func bindSpokePool(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := SpokePoolMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_SpokePool *SpokePoolRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _SpokePool.Contract.SpokePoolCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_SpokePool *SpokePoolRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _SpokePool.Contract.SpokePoolTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_SpokePool *SpokePoolRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _SpokePool.Contract.SpokePoolTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_SpokePool *SpokePoolCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _SpokePool.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_SpokePool *SpokePoolTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _SpokePool.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_SpokePool *SpokePoolTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _SpokePool.Contract.contract.Transact(opts, method, params...)
}

// EMPTYRELAYER is a free data retrieval call binding the contract method 0x6bbbcd2e.
//
// Solidity: function EMPTY_RELAYER() view returns(bytes32)
func (_SpokePool *SpokePoolCaller) EMPTYRELAYER(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "EMPTY_RELAYER")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// EMPTYRELAYER is a free data retrieval call binding the contract method 0x6bbbcd2e.
//
// Solidity: function EMPTY_RELAYER() view returns(bytes32)
func (_SpokePool *SpokePoolSession) EMPTYRELAYER() ([32]byte, error) {
	return _SpokePool.Contract.EMPTYRELAYER(&_SpokePool.CallOpts)
}

// EMPTYRELAYER is a free data retrieval call binding the contract method 0x6bbbcd2e.
//
// Solidity: function EMPTY_RELAYER() view returns(bytes32)
func (_SpokePool *SpokePoolCallerSession) EMPTYRELAYER() ([32]byte, error) {
	return _SpokePool.Contract.EMPTYRELAYER(&_SpokePool.CallOpts)
}

// EMPTYREPAYMENTCHAINID is a free data retrieval call binding the contract method 0x15348e44.
//
// Solidity: function EMPTY_REPAYMENT_CHAIN_ID() view returns(uint256)
func (_SpokePool *SpokePoolCaller) EMPTYREPAYMENTCHAINID(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "EMPTY_REPAYMENT_CHAIN_ID")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// EMPTYREPAYMENTCHAINID is a free data retrieval call binding the contract method 0x15348e44.
//
// Solidity: function EMPTY_REPAYMENT_CHAIN_ID() view returns(uint256)
func (_SpokePool *SpokePoolSession) EMPTYREPAYMENTCHAINID() (*big.Int, error) {
	return _SpokePool.Contract.EMPTYREPAYMENTCHAINID(&_SpokePool.CallOpts)
}

// EMPTYREPAYMENTCHAINID is a free data retrieval call binding the contract method 0x15348e44.
//
// Solidity: function EMPTY_REPAYMENT_CHAIN_ID() view returns(uint256)
func (_SpokePool *SpokePoolCallerSession) EMPTYREPAYMENTCHAINID() (*big.Int, error) {
	return _SpokePool.Contract.EMPTYREPAYMENTCHAINID(&_SpokePool.CallOpts)
}

// INFINITEFILLDEADLINE is a free data retrieval call binding the contract method 0xceb4c987.
//
// Solidity: function INFINITE_FILL_DEADLINE() view returns(uint32)
func (_SpokePool *SpokePoolCaller) INFINITEFILLDEADLINE(opts *bind.CallOpts) (uint32, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "INFINITE_FILL_DEADLINE")

	if err != nil {
		return *new(uint32), err
	}

	out0 := *abi.ConvertType(out[0], new(uint32)).(*uint32)

	return out0, err

}

// INFINITEFILLDEADLINE is a free data retrieval call binding the contract method 0xceb4c987.
//
// Solidity: function INFINITE_FILL_DEADLINE() view returns(uint32)
func (_SpokePool *SpokePoolSession) INFINITEFILLDEADLINE() (uint32, error) {
	return _SpokePool.Contract.INFINITEFILLDEADLINE(&_SpokePool.CallOpts)
}

// INFINITEFILLDEADLINE is a free data retrieval call binding the contract method 0xceb4c987.
//
// Solidity: function INFINITE_FILL_DEADLINE() view returns(uint32)
func (_SpokePool *SpokePoolCallerSession) INFINITEFILLDEADLINE() (uint32, error) {
	return _SpokePool.Contract.INFINITEFILLDEADLINE(&_SpokePool.CallOpts)
}

// MAXEXCLUSIVITYPERIODSECONDS is a free data retrieval call binding the contract method 0x490e49ef.
//
// Solidity: function MAX_EXCLUSIVITY_PERIOD_SECONDS() view returns(uint32)
func (_SpokePool *SpokePoolCaller) MAXEXCLUSIVITYPERIODSECONDS(opts *bind.CallOpts) (uint32, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "MAX_EXCLUSIVITY_PERIOD_SECONDS")

	if err != nil {
		return *new(uint32), err
	}

	out0 := *abi.ConvertType(out[0], new(uint32)).(*uint32)

	return out0, err

}

// MAXEXCLUSIVITYPERIODSECONDS is a free data retrieval call binding the contract method 0x490e49ef.
//
// Solidity: function MAX_EXCLUSIVITY_PERIOD_SECONDS() view returns(uint32)
func (_SpokePool *SpokePoolSession) MAXEXCLUSIVITYPERIODSECONDS() (uint32, error) {
	return _SpokePool.Contract.MAXEXCLUSIVITYPERIODSECONDS(&_SpokePool.CallOpts)
}

// MAXEXCLUSIVITYPERIODSECONDS is a free data retrieval call binding the contract method 0x490e49ef.
//
// Solidity: function MAX_EXCLUSIVITY_PERIOD_SECONDS() view returns(uint32)
func (_SpokePool *SpokePoolCallerSession) MAXEXCLUSIVITYPERIODSECONDS() (uint32, error) {
	return _SpokePool.Contract.MAXEXCLUSIVITYPERIODSECONDS(&_SpokePool.CallOpts)
}

// MAXTRANSFERSIZE is a free data retrieval call binding the contract method 0xddd224f1.
//
// Solidity: function MAX_TRANSFER_SIZE() view returns(uint256)
func (_SpokePool *SpokePoolCaller) MAXTRANSFERSIZE(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "MAX_TRANSFER_SIZE")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MAXTRANSFERSIZE is a free data retrieval call binding the contract method 0xddd224f1.
//
// Solidity: function MAX_TRANSFER_SIZE() view returns(uint256)
func (_SpokePool *SpokePoolSession) MAXTRANSFERSIZE() (*big.Int, error) {
	return _SpokePool.Contract.MAXTRANSFERSIZE(&_SpokePool.CallOpts)
}

// MAXTRANSFERSIZE is a free data retrieval call binding the contract method 0xddd224f1.
//
// Solidity: function MAX_TRANSFER_SIZE() view returns(uint256)
func (_SpokePool *SpokePoolCallerSession) MAXTRANSFERSIZE() (*big.Int, error) {
	return _SpokePool.Contract.MAXTRANSFERSIZE(&_SpokePool.CallOpts)
}

// UPDATEBYTES32DEPOSITDETAILSHASH is a free data retrieval call binding the contract method 0x670fa8ac.
//
// Solidity: function UPDATE_BYTES32_DEPOSIT_DETAILS_HASH() view returns(bytes32)
func (_SpokePool *SpokePoolCaller) UPDATEBYTES32DEPOSITDETAILSHASH(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "UPDATE_BYTES32_DEPOSIT_DETAILS_HASH")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// UPDATEBYTES32DEPOSITDETAILSHASH is a free data retrieval call binding the contract method 0x670fa8ac.
//
// Solidity: function UPDATE_BYTES32_DEPOSIT_DETAILS_HASH() view returns(bytes32)
func (_SpokePool *SpokePoolSession) UPDATEBYTES32DEPOSITDETAILSHASH() ([32]byte, error) {
	return _SpokePool.Contract.UPDATEBYTES32DEPOSITDETAILSHASH(&_SpokePool.CallOpts)
}

// UPDATEBYTES32DEPOSITDETAILSHASH is a free data retrieval call binding the contract method 0x670fa8ac.
//
// Solidity: function UPDATE_BYTES32_DEPOSIT_DETAILS_HASH() view returns(bytes32)
func (_SpokePool *SpokePoolCallerSession) UPDATEBYTES32DEPOSITDETAILSHASH() ([32]byte, error) {
	return _SpokePool.Contract.UPDATEBYTES32DEPOSITDETAILSHASH(&_SpokePool.CallOpts)
}

// ChainId is a free data retrieval call binding the contract method 0x9a8a0592.
//
// Solidity: function chainId() view returns(uint256)
func (_SpokePool *SpokePoolCaller) ChainId(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "chainId")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ChainId is a free data retrieval call binding the contract method 0x9a8a0592.
//
// Solidity: function chainId() view returns(uint256)
func (_SpokePool *SpokePoolSession) ChainId() (*big.Int, error) {
	return _SpokePool.Contract.ChainId(&_SpokePool.CallOpts)
}

// ChainId is a free data retrieval call binding the contract method 0x9a8a0592.
//
// Solidity: function chainId() view returns(uint256)
func (_SpokePool *SpokePoolCallerSession) ChainId() (*big.Int, error) {
	return _SpokePool.Contract.ChainId(&_SpokePool.CallOpts)
}

// CrossDomainAdmin is a free data retrieval call binding the contract method 0x5285e058.
//
// Solidity: function crossDomainAdmin() view returns(address)
func (_SpokePool *SpokePoolCaller) CrossDomainAdmin(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "crossDomainAdmin")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// CrossDomainAdmin is a free data retrieval call binding the contract method 0x5285e058.
//
// Solidity: function crossDomainAdmin() view returns(address)
func (_SpokePool *SpokePoolSession) CrossDomainAdmin() (common.Address, error) {
	return _SpokePool.Contract.CrossDomainAdmin(&_SpokePool.CallOpts)
}

// CrossDomainAdmin is a free data retrieval call binding the contract method 0x5285e058.
//
// Solidity: function crossDomainAdmin() view returns(address)
func (_SpokePool *SpokePoolCallerSession) CrossDomainAdmin() (common.Address, error) {
	return _SpokePool.Contract.CrossDomainAdmin(&_SpokePool.CallOpts)
}

// DepositQuoteTimeBuffer is a free data retrieval call binding the contract method 0x57f6dcb8.
//
// Solidity: function depositQuoteTimeBuffer() view returns(uint32)
func (_SpokePool *SpokePoolCaller) DepositQuoteTimeBuffer(opts *bind.CallOpts) (uint32, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "depositQuoteTimeBuffer")

	if err != nil {
		return *new(uint32), err
	}

	out0 := *abi.ConvertType(out[0], new(uint32)).(*uint32)

	return out0, err

}

// DepositQuoteTimeBuffer is a free data retrieval call binding the contract method 0x57f6dcb8.
//
// Solidity: function depositQuoteTimeBuffer() view returns(uint32)
func (_SpokePool *SpokePoolSession) DepositQuoteTimeBuffer() (uint32, error) {
	return _SpokePool.Contract.DepositQuoteTimeBuffer(&_SpokePool.CallOpts)
}

// DepositQuoteTimeBuffer is a free data retrieval call binding the contract method 0x57f6dcb8.
//
// Solidity: function depositQuoteTimeBuffer() view returns(uint32)
func (_SpokePool *SpokePoolCallerSession) DepositQuoteTimeBuffer() (uint32, error) {
	return _SpokePool.Contract.DepositQuoteTimeBuffer(&_SpokePool.CallOpts)
}

// FillDeadlineBuffer is a free data retrieval call binding the contract method 0x079bd2c7.
//
// Solidity: function fillDeadlineBuffer() view returns(uint32)
func (_SpokePool *SpokePoolCaller) FillDeadlineBuffer(opts *bind.CallOpts) (uint32, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "fillDeadlineBuffer")

	if err != nil {
		return *new(uint32), err
	}

	out0 := *abi.ConvertType(out[0], new(uint32)).(*uint32)

	return out0, err

}

// FillDeadlineBuffer is a free data retrieval call binding the contract method 0x079bd2c7.
//
// Solidity: function fillDeadlineBuffer() view returns(uint32)
func (_SpokePool *SpokePoolSession) FillDeadlineBuffer() (uint32, error) {
	return _SpokePool.Contract.FillDeadlineBuffer(&_SpokePool.CallOpts)
}

// FillDeadlineBuffer is a free data retrieval call binding the contract method 0x079bd2c7.
//
// Solidity: function fillDeadlineBuffer() view returns(uint32)
func (_SpokePool *SpokePoolCallerSession) FillDeadlineBuffer() (uint32, error) {
	return _SpokePool.Contract.FillDeadlineBuffer(&_SpokePool.CallOpts)
}

// FillStatuses is a free data retrieval call binding the contract method 0xc35c83fc.
//
// Solidity: function fillStatuses(bytes32 ) view returns(uint256)
func (_SpokePool *SpokePoolCaller) FillStatuses(opts *bind.CallOpts, arg0 [32]byte) (*big.Int, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "fillStatuses", arg0)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// FillStatuses is a free data retrieval call binding the contract method 0xc35c83fc.
//
// Solidity: function fillStatuses(bytes32 ) view returns(uint256)
func (_SpokePool *SpokePoolSession) FillStatuses(arg0 [32]byte) (*big.Int, error) {
	return _SpokePool.Contract.FillStatuses(&_SpokePool.CallOpts, arg0)
}

// FillStatuses is a free data retrieval call binding the contract method 0xc35c83fc.
//
// Solidity: function fillStatuses(bytes32 ) view returns(uint256)
func (_SpokePool *SpokePoolCallerSession) FillStatuses(arg0 [32]byte) (*big.Int, error) {
	return _SpokePool.Contract.FillStatuses(&_SpokePool.CallOpts, arg0)
}

// GetCurrentTime is a free data retrieval call binding the contract method 0x29cb924d.
//
// Solidity: function getCurrentTime() view returns(uint256)
func (_SpokePool *SpokePoolCaller) GetCurrentTime(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "getCurrentTime")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetCurrentTime is a free data retrieval call binding the contract method 0x29cb924d.
//
// Solidity: function getCurrentTime() view returns(uint256)
func (_SpokePool *SpokePoolSession) GetCurrentTime() (*big.Int, error) {
	return _SpokePool.Contract.GetCurrentTime(&_SpokePool.CallOpts)
}

// GetCurrentTime is a free data retrieval call binding the contract method 0x29cb924d.
//
// Solidity: function getCurrentTime() view returns(uint256)
func (_SpokePool *SpokePoolCallerSession) GetCurrentTime() (*big.Int, error) {
	return _SpokePool.Contract.GetCurrentTime(&_SpokePool.CallOpts)
}

// GetRelayerRefund is a free data retrieval call binding the contract method 0xadb5a6a6.
//
// Solidity: function getRelayerRefund(address l2TokenAddress, address refundAddress) view returns(uint256)
func (_SpokePool *SpokePoolCaller) GetRelayerRefund(opts *bind.CallOpts, l2TokenAddress common.Address, refundAddress common.Address) (*big.Int, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "getRelayerRefund", l2TokenAddress, refundAddress)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetRelayerRefund is a free data retrieval call binding the contract method 0xadb5a6a6.
//
// Solidity: function getRelayerRefund(address l2TokenAddress, address refundAddress) view returns(uint256)
func (_SpokePool *SpokePoolSession) GetRelayerRefund(l2TokenAddress common.Address, refundAddress common.Address) (*big.Int, error) {
	return _SpokePool.Contract.GetRelayerRefund(&_SpokePool.CallOpts, l2TokenAddress, refundAddress)
}

// GetRelayerRefund is a free data retrieval call binding the contract method 0xadb5a6a6.
//
// Solidity: function getRelayerRefund(address l2TokenAddress, address refundAddress) view returns(uint256)
func (_SpokePool *SpokePoolCallerSession) GetRelayerRefund(l2TokenAddress common.Address, refundAddress common.Address) (*big.Int, error) {
	return _SpokePool.Contract.GetRelayerRefund(&_SpokePool.CallOpts, l2TokenAddress, refundAddress)
}

// GetUnsafeDepositId is a free data retrieval call binding the contract method 0x7ef413e1.
//
// Solidity: function getUnsafeDepositId(address msgSender, bytes32 depositor, uint256 depositNonce) pure returns(uint256)
func (_SpokePool *SpokePoolCaller) GetUnsafeDepositId(opts *bind.CallOpts, msgSender common.Address, depositor [32]byte, depositNonce *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "getUnsafeDepositId", msgSender, depositor, depositNonce)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetUnsafeDepositId is a free data retrieval call binding the contract method 0x7ef413e1.
//
// Solidity: function getUnsafeDepositId(address msgSender, bytes32 depositor, uint256 depositNonce) pure returns(uint256)
func (_SpokePool *SpokePoolSession) GetUnsafeDepositId(msgSender common.Address, depositor [32]byte, depositNonce *big.Int) (*big.Int, error) {
	return _SpokePool.Contract.GetUnsafeDepositId(&_SpokePool.CallOpts, msgSender, depositor, depositNonce)
}

// GetUnsafeDepositId is a free data retrieval call binding the contract method 0x7ef413e1.
//
// Solidity: function getUnsafeDepositId(address msgSender, bytes32 depositor, uint256 depositNonce) pure returns(uint256)
func (_SpokePool *SpokePoolCallerSession) GetUnsafeDepositId(msgSender common.Address, depositor [32]byte, depositNonce *big.Int) (*big.Int, error) {
	return _SpokePool.Contract.GetUnsafeDepositId(&_SpokePool.CallOpts, msgSender, depositor, depositNonce)
}

// GetV3RelayHash is a free data retrieval call binding the contract method 0xd7e1583a.
//
// Solidity: function getV3RelayHash((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes) relayData) view returns(bytes32)
func (_SpokePool *SpokePoolCaller) GetV3RelayHash(opts *bind.CallOpts, relayData V3SpokePoolInterfaceV3RelayData) ([32]byte, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "getV3RelayHash", relayData)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// GetV3RelayHash is a free data retrieval call binding the contract method 0xd7e1583a.
//
// Solidity: function getV3RelayHash((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes) relayData) view returns(bytes32)
func (_SpokePool *SpokePoolSession) GetV3RelayHash(relayData V3SpokePoolInterfaceV3RelayData) ([32]byte, error) {
	return _SpokePool.Contract.GetV3RelayHash(&_SpokePool.CallOpts, relayData)
}

// GetV3RelayHash is a free data retrieval call binding the contract method 0xd7e1583a.
//
// Solidity: function getV3RelayHash((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes) relayData) view returns(bytes32)
func (_SpokePool *SpokePoolCallerSession) GetV3RelayHash(relayData V3SpokePoolInterfaceV3RelayData) ([32]byte, error) {
	return _SpokePool.Contract.GetV3RelayHash(&_SpokePool.CallOpts, relayData)
}

// NumberOfDeposits is a free data retrieval call binding the contract method 0xa1244c67.
//
// Solidity: function numberOfDeposits() view returns(uint32)
func (_SpokePool *SpokePoolCaller) NumberOfDeposits(opts *bind.CallOpts) (uint32, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "numberOfDeposits")

	if err != nil {
		return *new(uint32), err
	}

	out0 := *abi.ConvertType(out[0], new(uint32)).(*uint32)

	return out0, err

}

// NumberOfDeposits is a free data retrieval call binding the contract method 0xa1244c67.
//
// Solidity: function numberOfDeposits() view returns(uint32)
func (_SpokePool *SpokePoolSession) NumberOfDeposits() (uint32, error) {
	return _SpokePool.Contract.NumberOfDeposits(&_SpokePool.CallOpts)
}

// NumberOfDeposits is a free data retrieval call binding the contract method 0xa1244c67.
//
// Solidity: function numberOfDeposits() view returns(uint32)
func (_SpokePool *SpokePoolCallerSession) NumberOfDeposits() (uint32, error) {
	return _SpokePool.Contract.NumberOfDeposits(&_SpokePool.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_SpokePool *SpokePoolCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_SpokePool *SpokePoolSession) Owner() (common.Address, error) {
	return _SpokePool.Contract.Owner(&_SpokePool.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_SpokePool *SpokePoolCallerSession) Owner() (common.Address, error) {
	return _SpokePool.Contract.Owner(&_SpokePool.CallOpts)
}

// PausedDeposits is a free data retrieval call binding the contract method 0x6068d6cb.
//
// Solidity: function pausedDeposits() view returns(bool)
func (_SpokePool *SpokePoolCaller) PausedDeposits(opts *bind.CallOpts) (bool, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "pausedDeposits")

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// PausedDeposits is a free data retrieval call binding the contract method 0x6068d6cb.
//
// Solidity: function pausedDeposits() view returns(bool)
func (_SpokePool *SpokePoolSession) PausedDeposits() (bool, error) {
	return _SpokePool.Contract.PausedDeposits(&_SpokePool.CallOpts)
}

// PausedDeposits is a free data retrieval call binding the contract method 0x6068d6cb.
//
// Solidity: function pausedDeposits() view returns(bool)
func (_SpokePool *SpokePoolCallerSession) PausedDeposits() (bool, error) {
	return _SpokePool.Contract.PausedDeposits(&_SpokePool.CallOpts)
}

// PausedFills is a free data retrieval call binding the contract method 0xdda52113.
//
// Solidity: function pausedFills() view returns(bool)
func (_SpokePool *SpokePoolCaller) PausedFills(opts *bind.CallOpts) (bool, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "pausedFills")

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// PausedFills is a free data retrieval call binding the contract method 0xdda52113.
//
// Solidity: function pausedFills() view returns(bool)
func (_SpokePool *SpokePoolSession) PausedFills() (bool, error) {
	return _SpokePool.Contract.PausedFills(&_SpokePool.CallOpts)
}

// PausedFills is a free data retrieval call binding the contract method 0xdda52113.
//
// Solidity: function pausedFills() view returns(bool)
func (_SpokePool *SpokePoolCallerSession) PausedFills() (bool, error) {
	return _SpokePool.Contract.PausedFills(&_SpokePool.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_SpokePool *SpokePoolCaller) ProxiableUUID(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "proxiableUUID")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_SpokePool *SpokePoolSession) ProxiableUUID() ([32]byte, error) {
	return _SpokePool.Contract.ProxiableUUID(&_SpokePool.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_SpokePool *SpokePoolCallerSession) ProxiableUUID() ([32]byte, error) {
	return _SpokePool.Contract.ProxiableUUID(&_SpokePool.CallOpts)
}

// RelayerRefund is a free data retrieval call binding the contract method 0xf79f29ed.
//
// Solidity: function relayerRefund(address , address ) view returns(uint256)
func (_SpokePool *SpokePoolCaller) RelayerRefund(opts *bind.CallOpts, arg0 common.Address, arg1 common.Address) (*big.Int, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "relayerRefund", arg0, arg1)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// RelayerRefund is a free data retrieval call binding the contract method 0xf79f29ed.
//
// Solidity: function relayerRefund(address , address ) view returns(uint256)
func (_SpokePool *SpokePoolSession) RelayerRefund(arg0 common.Address, arg1 common.Address) (*big.Int, error) {
	return _SpokePool.Contract.RelayerRefund(&_SpokePool.CallOpts, arg0, arg1)
}

// RelayerRefund is a free data retrieval call binding the contract method 0xf79f29ed.
//
// Solidity: function relayerRefund(address , address ) view returns(uint256)
func (_SpokePool *SpokePoolCallerSession) RelayerRefund(arg0 common.Address, arg1 common.Address) (*big.Int, error) {
	return _SpokePool.Contract.RelayerRefund(&_SpokePool.CallOpts, arg0, arg1)
}

// RootBundles is a free data retrieval call binding the contract method 0xee2a53f8.
//
// Solidity: function rootBundles(uint256 ) view returns(bytes32 slowRelayRoot, bytes32 relayerRefundRoot)
func (_SpokePool *SpokePoolCaller) RootBundles(opts *bind.CallOpts, arg0 *big.Int) (struct {
	SlowRelayRoot     [32]byte
	RelayerRefundRoot [32]byte
}, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "rootBundles", arg0)

	outstruct := new(struct {
		SlowRelayRoot     [32]byte
		RelayerRefundRoot [32]byte
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.SlowRelayRoot = *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)
	outstruct.RelayerRefundRoot = *abi.ConvertType(out[1], new([32]byte)).(*[32]byte)

	return *outstruct, err

}

// RootBundles is a free data retrieval call binding the contract method 0xee2a53f8.
//
// Solidity: function rootBundles(uint256 ) view returns(bytes32 slowRelayRoot, bytes32 relayerRefundRoot)
func (_SpokePool *SpokePoolSession) RootBundles(arg0 *big.Int) (struct {
	SlowRelayRoot     [32]byte
	RelayerRefundRoot [32]byte
}, error) {
	return _SpokePool.Contract.RootBundles(&_SpokePool.CallOpts, arg0)
}

// RootBundles is a free data retrieval call binding the contract method 0xee2a53f8.
//
// Solidity: function rootBundles(uint256 ) view returns(bytes32 slowRelayRoot, bytes32 relayerRefundRoot)
func (_SpokePool *SpokePoolCallerSession) RootBundles(arg0 *big.Int) (struct {
	SlowRelayRoot     [32]byte
	RelayerRefundRoot [32]byte
}, error) {
	return _SpokePool.Contract.RootBundles(&_SpokePool.CallOpts, arg0)
}

// WithdrawalRecipient is a free data retrieval call binding the contract method 0xb370b7f5.
//
// Solidity: function withdrawalRecipient() view returns(address)
func (_SpokePool *SpokePoolCaller) WithdrawalRecipient(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "withdrawalRecipient")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// WithdrawalRecipient is a free data retrieval call binding the contract method 0xb370b7f5.
//
// Solidity: function withdrawalRecipient() view returns(address)
func (_SpokePool *SpokePoolSession) WithdrawalRecipient() (common.Address, error) {
	return _SpokePool.Contract.WithdrawalRecipient(&_SpokePool.CallOpts)
}

// WithdrawalRecipient is a free data retrieval call binding the contract method 0xb370b7f5.
//
// Solidity: function withdrawalRecipient() view returns(address)
func (_SpokePool *SpokePoolCallerSession) WithdrawalRecipient() (common.Address, error) {
	return _SpokePool.Contract.WithdrawalRecipient(&_SpokePool.CallOpts)
}

// WrappedNativeToken is a free data retrieval call binding the contract method 0x17fcb39b.
//
// Solidity: function wrappedNativeToken() view returns(address)
func (_SpokePool *SpokePoolCaller) WrappedNativeToken(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SpokePool.contract.Call(opts, &out, "wrappedNativeToken")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// WrappedNativeToken is a free data retrieval call binding the contract method 0x17fcb39b.
//
// Solidity: function wrappedNativeToken() view returns(address)
func (_SpokePool *SpokePoolSession) WrappedNativeToken() (common.Address, error) {
	return _SpokePool.Contract.WrappedNativeToken(&_SpokePool.CallOpts)
}

// WrappedNativeToken is a free data retrieval call binding the contract method 0x17fcb39b.
//
// Solidity: function wrappedNativeToken() view returns(address)
func (_SpokePool *SpokePoolCallerSession) WrappedNativeToken() (common.Address, error) {
	return _SpokePool.Contract.WrappedNativeToken(&_SpokePool.CallOpts)
}

// SpokePoolInit is a paid mutator transaction binding the contract method 0x979f2bc2.
//
// Solidity: function __SpokePool_init(uint32 _initialDepositId, address _crossDomainAdmin, address _withdrawalRecipient) returns()
func (_SpokePool *SpokePoolTransactor) SpokePoolInit(opts *bind.TransactOpts, _initialDepositId uint32, _crossDomainAdmin common.Address, _withdrawalRecipient common.Address) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "__SpokePool_init", _initialDepositId, _crossDomainAdmin, _withdrawalRecipient)
}

// SpokePoolInit is a paid mutator transaction binding the contract method 0x979f2bc2.
//
// Solidity: function __SpokePool_init(uint32 _initialDepositId, address _crossDomainAdmin, address _withdrawalRecipient) returns()
func (_SpokePool *SpokePoolSession) SpokePoolInit(_initialDepositId uint32, _crossDomainAdmin common.Address, _withdrawalRecipient common.Address) (*types.Transaction, error) {
	return _SpokePool.Contract.SpokePoolInit(&_SpokePool.TransactOpts, _initialDepositId, _crossDomainAdmin, _withdrawalRecipient)
}

// SpokePoolInit is a paid mutator transaction binding the contract method 0x979f2bc2.
//
// Solidity: function __SpokePool_init(uint32 _initialDepositId, address _crossDomainAdmin, address _withdrawalRecipient) returns()
func (_SpokePool *SpokePoolTransactorSession) SpokePoolInit(_initialDepositId uint32, _crossDomainAdmin common.Address, _withdrawalRecipient common.Address) (*types.Transaction, error) {
	return _SpokePool.Contract.SpokePoolInit(&_SpokePool.TransactOpts, _initialDepositId, _crossDomainAdmin, _withdrawalRecipient)
}

// ClaimRelayerRefund is a paid mutator transaction binding the contract method 0xa18a096e.
//
// Solidity: function claimRelayerRefund(bytes32 l2TokenAddress, bytes32 refundAddress) returns()
func (_SpokePool *SpokePoolTransactor) ClaimRelayerRefund(opts *bind.TransactOpts, l2TokenAddress [32]byte, refundAddress [32]byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "claimRelayerRefund", l2TokenAddress, refundAddress)
}

// ClaimRelayerRefund is a paid mutator transaction binding the contract method 0xa18a096e.
//
// Solidity: function claimRelayerRefund(bytes32 l2TokenAddress, bytes32 refundAddress) returns()
func (_SpokePool *SpokePoolSession) ClaimRelayerRefund(l2TokenAddress [32]byte, refundAddress [32]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.ClaimRelayerRefund(&_SpokePool.TransactOpts, l2TokenAddress, refundAddress)
}

// ClaimRelayerRefund is a paid mutator transaction binding the contract method 0xa18a096e.
//
// Solidity: function claimRelayerRefund(bytes32 l2TokenAddress, bytes32 refundAddress) returns()
func (_SpokePool *SpokePoolTransactorSession) ClaimRelayerRefund(l2TokenAddress [32]byte, refundAddress [32]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.ClaimRelayerRefund(&_SpokePool.TransactOpts, l2TokenAddress, refundAddress)
}

// Deposit is a paid mutator transaction binding the contract method 0xad5425c6.
//
// Solidity: function deposit(bytes32 depositor, bytes32 recipient, bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, bytes32 exclusiveRelayer, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolTransactor) Deposit(opts *bind.TransactOpts, depositor [32]byte, recipient [32]byte, inputToken [32]byte, outputToken [32]byte, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer [32]byte, quoteTimestamp uint32, fillDeadline uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "deposit", depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, quoteTimestamp, fillDeadline, exclusivityParameter, message)
}

// Deposit is a paid mutator transaction binding the contract method 0xad5425c6.
//
// Solidity: function deposit(bytes32 depositor, bytes32 recipient, bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, bytes32 exclusiveRelayer, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolSession) Deposit(depositor [32]byte, recipient [32]byte, inputToken [32]byte, outputToken [32]byte, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer [32]byte, quoteTimestamp uint32, fillDeadline uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.Deposit(&_SpokePool.TransactOpts, depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, quoteTimestamp, fillDeadline, exclusivityParameter, message)
}

// Deposit is a paid mutator transaction binding the contract method 0xad5425c6.
//
// Solidity: function deposit(bytes32 depositor, bytes32 recipient, bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, bytes32 exclusiveRelayer, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolTransactorSession) Deposit(depositor [32]byte, recipient [32]byte, inputToken [32]byte, outputToken [32]byte, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer [32]byte, quoteTimestamp uint32, fillDeadline uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.Deposit(&_SpokePool.TransactOpts, depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, quoteTimestamp, fillDeadline, exclusivityParameter, message)
}

// DepositDeprecated5947912356 is a paid mutator transaction binding the contract method 0x1186ec33.
//
// Solidity: function depositDeprecated_5947912356(address recipient, address originToken, uint256 amount, uint256 destinationChainId, int64 relayerFeePct, uint32 quoteTimestamp, bytes message, uint256 ) payable returns()
func (_SpokePool *SpokePoolTransactor) DepositDeprecated5947912356(opts *bind.TransactOpts, recipient common.Address, originToken common.Address, amount *big.Int, destinationChainId *big.Int, relayerFeePct int64, quoteTimestamp uint32, message []byte, arg7 *big.Int) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "depositDeprecated_5947912356", recipient, originToken, amount, destinationChainId, relayerFeePct, quoteTimestamp, message, arg7)
}

// DepositDeprecated5947912356 is a paid mutator transaction binding the contract method 0x1186ec33.
//
// Solidity: function depositDeprecated_5947912356(address recipient, address originToken, uint256 amount, uint256 destinationChainId, int64 relayerFeePct, uint32 quoteTimestamp, bytes message, uint256 ) payable returns()
func (_SpokePool *SpokePoolSession) DepositDeprecated5947912356(recipient common.Address, originToken common.Address, amount *big.Int, destinationChainId *big.Int, relayerFeePct int64, quoteTimestamp uint32, message []byte, arg7 *big.Int) (*types.Transaction, error) {
	return _SpokePool.Contract.DepositDeprecated5947912356(&_SpokePool.TransactOpts, recipient, originToken, amount, destinationChainId, relayerFeePct, quoteTimestamp, message, arg7)
}

// DepositDeprecated5947912356 is a paid mutator transaction binding the contract method 0x1186ec33.
//
// Solidity: function depositDeprecated_5947912356(address recipient, address originToken, uint256 amount, uint256 destinationChainId, int64 relayerFeePct, uint32 quoteTimestamp, bytes message, uint256 ) payable returns()
func (_SpokePool *SpokePoolTransactorSession) DepositDeprecated5947912356(recipient common.Address, originToken common.Address, amount *big.Int, destinationChainId *big.Int, relayerFeePct int64, quoteTimestamp uint32, message []byte, arg7 *big.Int) (*types.Transaction, error) {
	return _SpokePool.Contract.DepositDeprecated5947912356(&_SpokePool.TransactOpts, recipient, originToken, amount, destinationChainId, relayerFeePct, quoteTimestamp, message, arg7)
}

// DepositFor is a paid mutator transaction binding the contract method 0x541f4f14.
//
// Solidity: function depositFor(address depositor, address recipient, address originToken, uint256 amount, uint256 destinationChainId, int64 relayerFeePct, uint32 quoteTimestamp, bytes message, uint256 ) payable returns()
func (_SpokePool *SpokePoolTransactor) DepositFor(opts *bind.TransactOpts, depositor common.Address, recipient common.Address, originToken common.Address, amount *big.Int, destinationChainId *big.Int, relayerFeePct int64, quoteTimestamp uint32, message []byte, arg8 *big.Int) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "depositFor", depositor, recipient, originToken, amount, destinationChainId, relayerFeePct, quoteTimestamp, message, arg8)
}

// DepositFor is a paid mutator transaction binding the contract method 0x541f4f14.
//
// Solidity: function depositFor(address depositor, address recipient, address originToken, uint256 amount, uint256 destinationChainId, int64 relayerFeePct, uint32 quoteTimestamp, bytes message, uint256 ) payable returns()
func (_SpokePool *SpokePoolSession) DepositFor(depositor common.Address, recipient common.Address, originToken common.Address, amount *big.Int, destinationChainId *big.Int, relayerFeePct int64, quoteTimestamp uint32, message []byte, arg8 *big.Int) (*types.Transaction, error) {
	return _SpokePool.Contract.DepositFor(&_SpokePool.TransactOpts, depositor, recipient, originToken, amount, destinationChainId, relayerFeePct, quoteTimestamp, message, arg8)
}

// DepositFor is a paid mutator transaction binding the contract method 0x541f4f14.
//
// Solidity: function depositFor(address depositor, address recipient, address originToken, uint256 amount, uint256 destinationChainId, int64 relayerFeePct, uint32 quoteTimestamp, bytes message, uint256 ) payable returns()
func (_SpokePool *SpokePoolTransactorSession) DepositFor(depositor common.Address, recipient common.Address, originToken common.Address, amount *big.Int, destinationChainId *big.Int, relayerFeePct int64, quoteTimestamp uint32, message []byte, arg8 *big.Int) (*types.Transaction, error) {
	return _SpokePool.Contract.DepositFor(&_SpokePool.TransactOpts, depositor, recipient, originToken, amount, destinationChainId, relayerFeePct, quoteTimestamp, message, arg8)
}

// DepositNow is a paid mutator transaction binding the contract method 0xea86bd46.
//
// Solidity: function depositNow(bytes32 depositor, bytes32 recipient, bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, bytes32 exclusiveRelayer, uint32 fillDeadlineOffset, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolTransactor) DepositNow(opts *bind.TransactOpts, depositor [32]byte, recipient [32]byte, inputToken [32]byte, outputToken [32]byte, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer [32]byte, fillDeadlineOffset uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "depositNow", depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, fillDeadlineOffset, exclusivityParameter, message)
}

// DepositNow is a paid mutator transaction binding the contract method 0xea86bd46.
//
// Solidity: function depositNow(bytes32 depositor, bytes32 recipient, bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, bytes32 exclusiveRelayer, uint32 fillDeadlineOffset, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolSession) DepositNow(depositor [32]byte, recipient [32]byte, inputToken [32]byte, outputToken [32]byte, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer [32]byte, fillDeadlineOffset uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.DepositNow(&_SpokePool.TransactOpts, depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, fillDeadlineOffset, exclusivityParameter, message)
}

// DepositNow is a paid mutator transaction binding the contract method 0xea86bd46.
//
// Solidity: function depositNow(bytes32 depositor, bytes32 recipient, bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, bytes32 exclusiveRelayer, uint32 fillDeadlineOffset, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolTransactorSession) DepositNow(depositor [32]byte, recipient [32]byte, inputToken [32]byte, outputToken [32]byte, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer [32]byte, fillDeadlineOffset uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.DepositNow(&_SpokePool.TransactOpts, depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, fillDeadlineOffset, exclusivityParameter, message)
}

// DepositV3 is a paid mutator transaction binding the contract method 0x7b939232.
//
// Solidity: function depositV3(address depositor, address recipient, address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, address exclusiveRelayer, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolTransactor) DepositV3(opts *bind.TransactOpts, depositor common.Address, recipient common.Address, inputToken common.Address, outputToken common.Address, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer common.Address, quoteTimestamp uint32, fillDeadline uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "depositV3", depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, quoteTimestamp, fillDeadline, exclusivityParameter, message)
}

// DepositV3 is a paid mutator transaction binding the contract method 0x7b939232.
//
// Solidity: function depositV3(address depositor, address recipient, address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, address exclusiveRelayer, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolSession) DepositV3(depositor common.Address, recipient common.Address, inputToken common.Address, outputToken common.Address, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer common.Address, quoteTimestamp uint32, fillDeadline uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.DepositV3(&_SpokePool.TransactOpts, depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, quoteTimestamp, fillDeadline, exclusivityParameter, message)
}

// DepositV3 is a paid mutator transaction binding the contract method 0x7b939232.
//
// Solidity: function depositV3(address depositor, address recipient, address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, address exclusiveRelayer, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolTransactorSession) DepositV3(depositor common.Address, recipient common.Address, inputToken common.Address, outputToken common.Address, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer common.Address, quoteTimestamp uint32, fillDeadline uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.DepositV3(&_SpokePool.TransactOpts, depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, quoteTimestamp, fillDeadline, exclusivityParameter, message)
}

// DepositV3Now is a paid mutator transaction binding the contract method 0x7aef642c.
//
// Solidity: function depositV3Now(address depositor, address recipient, address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, address exclusiveRelayer, uint32 fillDeadlineOffset, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolTransactor) DepositV3Now(opts *bind.TransactOpts, depositor common.Address, recipient common.Address, inputToken common.Address, outputToken common.Address, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer common.Address, fillDeadlineOffset uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "depositV3Now", depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, fillDeadlineOffset, exclusivityParameter, message)
}

// DepositV3Now is a paid mutator transaction binding the contract method 0x7aef642c.
//
// Solidity: function depositV3Now(address depositor, address recipient, address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, address exclusiveRelayer, uint32 fillDeadlineOffset, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolSession) DepositV3Now(depositor common.Address, recipient common.Address, inputToken common.Address, outputToken common.Address, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer common.Address, fillDeadlineOffset uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.DepositV3Now(&_SpokePool.TransactOpts, depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, fillDeadlineOffset, exclusivityParameter, message)
}

// DepositV3Now is a paid mutator transaction binding the contract method 0x7aef642c.
//
// Solidity: function depositV3Now(address depositor, address recipient, address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, address exclusiveRelayer, uint32 fillDeadlineOffset, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolTransactorSession) DepositV3Now(depositor common.Address, recipient common.Address, inputToken common.Address, outputToken common.Address, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer common.Address, fillDeadlineOffset uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.DepositV3Now(&_SpokePool.TransactOpts, depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, fillDeadlineOffset, exclusivityParameter, message)
}

// EmergencyDeleteRootBundle is a paid mutator transaction binding the contract method 0x8a7860ce.
//
// Solidity: function emergencyDeleteRootBundle(uint256 rootBundleId) returns()
func (_SpokePool *SpokePoolTransactor) EmergencyDeleteRootBundle(opts *bind.TransactOpts, rootBundleId *big.Int) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "emergencyDeleteRootBundle", rootBundleId)
}

// EmergencyDeleteRootBundle is a paid mutator transaction binding the contract method 0x8a7860ce.
//
// Solidity: function emergencyDeleteRootBundle(uint256 rootBundleId) returns()
func (_SpokePool *SpokePoolSession) EmergencyDeleteRootBundle(rootBundleId *big.Int) (*types.Transaction, error) {
	return _SpokePool.Contract.EmergencyDeleteRootBundle(&_SpokePool.TransactOpts, rootBundleId)
}

// EmergencyDeleteRootBundle is a paid mutator transaction binding the contract method 0x8a7860ce.
//
// Solidity: function emergencyDeleteRootBundle(uint256 rootBundleId) returns()
func (_SpokePool *SpokePoolTransactorSession) EmergencyDeleteRootBundle(rootBundleId *big.Int) (*types.Transaction, error) {
	return _SpokePool.Contract.EmergencyDeleteRootBundle(&_SpokePool.TransactOpts, rootBundleId)
}

// ExecuteRelayerRefundLeaf is a paid mutator transaction binding the contract method 0x1b3d5559.
//
// Solidity: function executeRelayerRefundLeaf(uint32 rootBundleId, (uint256,uint256,uint256[],uint32,address,address[]) relayerRefundLeaf, bytes32[] proof) payable returns()
func (_SpokePool *SpokePoolTransactor) ExecuteRelayerRefundLeaf(opts *bind.TransactOpts, rootBundleId uint32, relayerRefundLeaf SpokePoolInterfaceRelayerRefundLeaf, proof [][32]byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "executeRelayerRefundLeaf", rootBundleId, relayerRefundLeaf, proof)
}

// ExecuteRelayerRefundLeaf is a paid mutator transaction binding the contract method 0x1b3d5559.
//
// Solidity: function executeRelayerRefundLeaf(uint32 rootBundleId, (uint256,uint256,uint256[],uint32,address,address[]) relayerRefundLeaf, bytes32[] proof) payable returns()
func (_SpokePool *SpokePoolSession) ExecuteRelayerRefundLeaf(rootBundleId uint32, relayerRefundLeaf SpokePoolInterfaceRelayerRefundLeaf, proof [][32]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.ExecuteRelayerRefundLeaf(&_SpokePool.TransactOpts, rootBundleId, relayerRefundLeaf, proof)
}

// ExecuteRelayerRefundLeaf is a paid mutator transaction binding the contract method 0x1b3d5559.
//
// Solidity: function executeRelayerRefundLeaf(uint32 rootBundleId, (uint256,uint256,uint256[],uint32,address,address[]) relayerRefundLeaf, bytes32[] proof) payable returns()
func (_SpokePool *SpokePoolTransactorSession) ExecuteRelayerRefundLeaf(rootBundleId uint32, relayerRefundLeaf SpokePoolInterfaceRelayerRefundLeaf, proof [][32]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.ExecuteRelayerRefundLeaf(&_SpokePool.TransactOpts, rootBundleId, relayerRefundLeaf, proof)
}

// ExecuteSlowRelayLeaf is a paid mutator transaction binding the contract method 0x1fab657c.
//
// Solidity: function executeSlowRelayLeaf(((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes),uint256,uint256) slowFillLeaf, uint32 rootBundleId, bytes32[] proof) returns()
func (_SpokePool *SpokePoolTransactor) ExecuteSlowRelayLeaf(opts *bind.TransactOpts, slowFillLeaf V3SpokePoolInterfaceV3SlowFill, rootBundleId uint32, proof [][32]byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "executeSlowRelayLeaf", slowFillLeaf, rootBundleId, proof)
}

// ExecuteSlowRelayLeaf is a paid mutator transaction binding the contract method 0x1fab657c.
//
// Solidity: function executeSlowRelayLeaf(((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes),uint256,uint256) slowFillLeaf, uint32 rootBundleId, bytes32[] proof) returns()
func (_SpokePool *SpokePoolSession) ExecuteSlowRelayLeaf(slowFillLeaf V3SpokePoolInterfaceV3SlowFill, rootBundleId uint32, proof [][32]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.ExecuteSlowRelayLeaf(&_SpokePool.TransactOpts, slowFillLeaf, rootBundleId, proof)
}

// ExecuteSlowRelayLeaf is a paid mutator transaction binding the contract method 0x1fab657c.
//
// Solidity: function executeSlowRelayLeaf(((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes),uint256,uint256) slowFillLeaf, uint32 rootBundleId, bytes32[] proof) returns()
func (_SpokePool *SpokePoolTransactorSession) ExecuteSlowRelayLeaf(slowFillLeaf V3SpokePoolInterfaceV3SlowFill, rootBundleId uint32, proof [][32]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.ExecuteSlowRelayLeaf(&_SpokePool.TransactOpts, slowFillLeaf, rootBundleId, proof)
}

// Fill is a paid mutator transaction binding the contract method 0x82e2c43f.
//
// Solidity: function fill(bytes32 orderId, bytes originData, bytes fillerData) returns()
func (_SpokePool *SpokePoolTransactor) Fill(opts *bind.TransactOpts, orderId [32]byte, originData []byte, fillerData []byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "fill", orderId, originData, fillerData)
}

// Fill is a paid mutator transaction binding the contract method 0x82e2c43f.
//
// Solidity: function fill(bytes32 orderId, bytes originData, bytes fillerData) returns()
func (_SpokePool *SpokePoolSession) Fill(orderId [32]byte, originData []byte, fillerData []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.Fill(&_SpokePool.TransactOpts, orderId, originData, fillerData)
}

// Fill is a paid mutator transaction binding the contract method 0x82e2c43f.
//
// Solidity: function fill(bytes32 orderId, bytes originData, bytes fillerData) returns()
func (_SpokePool *SpokePoolTransactorSession) Fill(orderId [32]byte, originData []byte, fillerData []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.Fill(&_SpokePool.TransactOpts, orderId, originData, fillerData)
}

// FillRelay is a paid mutator transaction binding the contract method 0xdeff4b24.
//
// Solidity: function fillRelay((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes) relayData, uint256 repaymentChainId, bytes32 repaymentAddress) returns()
func (_SpokePool *SpokePoolTransactor) FillRelay(opts *bind.TransactOpts, relayData V3SpokePoolInterfaceV3RelayData, repaymentChainId *big.Int, repaymentAddress [32]byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "fillRelay", relayData, repaymentChainId, repaymentAddress)
}

// FillRelay is a paid mutator transaction binding the contract method 0xdeff4b24.
//
// Solidity: function fillRelay((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes) relayData, uint256 repaymentChainId, bytes32 repaymentAddress) returns()
func (_SpokePool *SpokePoolSession) FillRelay(relayData V3SpokePoolInterfaceV3RelayData, repaymentChainId *big.Int, repaymentAddress [32]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.FillRelay(&_SpokePool.TransactOpts, relayData, repaymentChainId, repaymentAddress)
}

// FillRelay is a paid mutator transaction binding the contract method 0xdeff4b24.
//
// Solidity: function fillRelay((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes) relayData, uint256 repaymentChainId, bytes32 repaymentAddress) returns()
func (_SpokePool *SpokePoolTransactorSession) FillRelay(relayData V3SpokePoolInterfaceV3RelayData, repaymentChainId *big.Int, repaymentAddress [32]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.FillRelay(&_SpokePool.TransactOpts, relayData, repaymentChainId, repaymentAddress)
}

// FillRelayWithUpdatedDeposit is a paid mutator transaction binding the contract method 0x97943aa9.
//
// Solidity: function fillRelayWithUpdatedDeposit((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes) relayData, uint256 repaymentChainId, bytes32 repaymentAddress, uint256 updatedOutputAmount, bytes32 updatedRecipient, bytes updatedMessage, bytes depositorSignature) returns()
func (_SpokePool *SpokePoolTransactor) FillRelayWithUpdatedDeposit(opts *bind.TransactOpts, relayData V3SpokePoolInterfaceV3RelayData, repaymentChainId *big.Int, repaymentAddress [32]byte, updatedOutputAmount *big.Int, updatedRecipient [32]byte, updatedMessage []byte, depositorSignature []byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "fillRelayWithUpdatedDeposit", relayData, repaymentChainId, repaymentAddress, updatedOutputAmount, updatedRecipient, updatedMessage, depositorSignature)
}

// FillRelayWithUpdatedDeposit is a paid mutator transaction binding the contract method 0x97943aa9.
//
// Solidity: function fillRelayWithUpdatedDeposit((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes) relayData, uint256 repaymentChainId, bytes32 repaymentAddress, uint256 updatedOutputAmount, bytes32 updatedRecipient, bytes updatedMessage, bytes depositorSignature) returns()
func (_SpokePool *SpokePoolSession) FillRelayWithUpdatedDeposit(relayData V3SpokePoolInterfaceV3RelayData, repaymentChainId *big.Int, repaymentAddress [32]byte, updatedOutputAmount *big.Int, updatedRecipient [32]byte, updatedMessage []byte, depositorSignature []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.FillRelayWithUpdatedDeposit(&_SpokePool.TransactOpts, relayData, repaymentChainId, repaymentAddress, updatedOutputAmount, updatedRecipient, updatedMessage, depositorSignature)
}

// FillRelayWithUpdatedDeposit is a paid mutator transaction binding the contract method 0x97943aa9.
//
// Solidity: function fillRelayWithUpdatedDeposit((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes) relayData, uint256 repaymentChainId, bytes32 repaymentAddress, uint256 updatedOutputAmount, bytes32 updatedRecipient, bytes updatedMessage, bytes depositorSignature) returns()
func (_SpokePool *SpokePoolTransactorSession) FillRelayWithUpdatedDeposit(relayData V3SpokePoolInterfaceV3RelayData, repaymentChainId *big.Int, repaymentAddress [32]byte, updatedOutputAmount *big.Int, updatedRecipient [32]byte, updatedMessage []byte, depositorSignature []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.FillRelayWithUpdatedDeposit(&_SpokePool.TransactOpts, relayData, repaymentChainId, repaymentAddress, updatedOutputAmount, updatedRecipient, updatedMessage, depositorSignature)
}

// FillV3Relay is a paid mutator transaction binding the contract method 0x2e378115.
//
// Solidity: function fillV3Relay((address,address,address,address,address,uint256,uint256,uint256,uint32,uint32,uint32,bytes) relayData, uint256 repaymentChainId) returns()
func (_SpokePool *SpokePoolTransactor) FillV3Relay(opts *bind.TransactOpts, relayData V3SpokePoolInterfaceV3RelayDataLegacy, repaymentChainId *big.Int) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "fillV3Relay", relayData, repaymentChainId)
}

// FillV3Relay is a paid mutator transaction binding the contract method 0x2e378115.
//
// Solidity: function fillV3Relay((address,address,address,address,address,uint256,uint256,uint256,uint32,uint32,uint32,bytes) relayData, uint256 repaymentChainId) returns()
func (_SpokePool *SpokePoolSession) FillV3Relay(relayData V3SpokePoolInterfaceV3RelayDataLegacy, repaymentChainId *big.Int) (*types.Transaction, error) {
	return _SpokePool.Contract.FillV3Relay(&_SpokePool.TransactOpts, relayData, repaymentChainId)
}

// FillV3Relay is a paid mutator transaction binding the contract method 0x2e378115.
//
// Solidity: function fillV3Relay((address,address,address,address,address,uint256,uint256,uint256,uint32,uint32,uint32,bytes) relayData, uint256 repaymentChainId) returns()
func (_SpokePool *SpokePoolTransactorSession) FillV3Relay(relayData V3SpokePoolInterfaceV3RelayDataLegacy, repaymentChainId *big.Int) (*types.Transaction, error) {
	return _SpokePool.Contract.FillV3Relay(&_SpokePool.TransactOpts, relayData, repaymentChainId)
}

// Initialize is a paid mutator transaction binding the contract method 0x8624c35c.
//
// Solidity: function initialize(uint32 _initialDepositId, address _withdrawalRecipient) returns()
func (_SpokePool *SpokePoolTransactor) Initialize(opts *bind.TransactOpts, _initialDepositId uint32, _withdrawalRecipient common.Address) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "initialize", _initialDepositId, _withdrawalRecipient)
}

// Initialize is a paid mutator transaction binding the contract method 0x8624c35c.
//
// Solidity: function initialize(uint32 _initialDepositId, address _withdrawalRecipient) returns()
func (_SpokePool *SpokePoolSession) Initialize(_initialDepositId uint32, _withdrawalRecipient common.Address) (*types.Transaction, error) {
	return _SpokePool.Contract.Initialize(&_SpokePool.TransactOpts, _initialDepositId, _withdrawalRecipient)
}

// Initialize is a paid mutator transaction binding the contract method 0x8624c35c.
//
// Solidity: function initialize(uint32 _initialDepositId, address _withdrawalRecipient) returns()
func (_SpokePool *SpokePoolTransactorSession) Initialize(_initialDepositId uint32, _withdrawalRecipient common.Address) (*types.Transaction, error) {
	return _SpokePool.Contract.Initialize(&_SpokePool.TransactOpts, _initialDepositId, _withdrawalRecipient)
}

// Multicall is a paid mutator transaction binding the contract method 0xac9650d8.
//
// Solidity: function multicall(bytes[] data) returns(bytes[] results)
func (_SpokePool *SpokePoolTransactor) Multicall(opts *bind.TransactOpts, data [][]byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "multicall", data)
}

// Multicall is a paid mutator transaction binding the contract method 0xac9650d8.
//
// Solidity: function multicall(bytes[] data) returns(bytes[] results)
func (_SpokePool *SpokePoolSession) Multicall(data [][]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.Multicall(&_SpokePool.TransactOpts, data)
}

// Multicall is a paid mutator transaction binding the contract method 0xac9650d8.
//
// Solidity: function multicall(bytes[] data) returns(bytes[] results)
func (_SpokePool *SpokePoolTransactorSession) Multicall(data [][]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.Multicall(&_SpokePool.TransactOpts, data)
}

// PauseDeposits is a paid mutator transaction binding the contract method 0x738b62e5.
//
// Solidity: function pauseDeposits(bool pause) returns()
func (_SpokePool *SpokePoolTransactor) PauseDeposits(opts *bind.TransactOpts, pause bool) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "pauseDeposits", pause)
}

// PauseDeposits is a paid mutator transaction binding the contract method 0x738b62e5.
//
// Solidity: function pauseDeposits(bool pause) returns()
func (_SpokePool *SpokePoolSession) PauseDeposits(pause bool) (*types.Transaction, error) {
	return _SpokePool.Contract.PauseDeposits(&_SpokePool.TransactOpts, pause)
}

// PauseDeposits is a paid mutator transaction binding the contract method 0x738b62e5.
//
// Solidity: function pauseDeposits(bool pause) returns()
func (_SpokePool *SpokePoolTransactorSession) PauseDeposits(pause bool) (*types.Transaction, error) {
	return _SpokePool.Contract.PauseDeposits(&_SpokePool.TransactOpts, pause)
}

// PauseFills is a paid mutator transaction binding the contract method 0x99cc2968.
//
// Solidity: function pauseFills(bool pause) returns()
func (_SpokePool *SpokePoolTransactor) PauseFills(opts *bind.TransactOpts, pause bool) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "pauseFills", pause)
}

// PauseFills is a paid mutator transaction binding the contract method 0x99cc2968.
//
// Solidity: function pauseFills(bool pause) returns()
func (_SpokePool *SpokePoolSession) PauseFills(pause bool) (*types.Transaction, error) {
	return _SpokePool.Contract.PauseFills(&_SpokePool.TransactOpts, pause)
}

// PauseFills is a paid mutator transaction binding the contract method 0x99cc2968.
//
// Solidity: function pauseFills(bool pause) returns()
func (_SpokePool *SpokePoolTransactorSession) PauseFills(pause bool) (*types.Transaction, error) {
	return _SpokePool.Contract.PauseFills(&_SpokePool.TransactOpts, pause)
}

// RelayRootBundle is a paid mutator transaction binding the contract method 0x493a4f84.
//
// Solidity: function relayRootBundle(bytes32 relayerRefundRoot, bytes32 slowRelayRoot) returns()
func (_SpokePool *SpokePoolTransactor) RelayRootBundle(opts *bind.TransactOpts, relayerRefundRoot [32]byte, slowRelayRoot [32]byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "relayRootBundle", relayerRefundRoot, slowRelayRoot)
}

// RelayRootBundle is a paid mutator transaction binding the contract method 0x493a4f84.
//
// Solidity: function relayRootBundle(bytes32 relayerRefundRoot, bytes32 slowRelayRoot) returns()
func (_SpokePool *SpokePoolSession) RelayRootBundle(relayerRefundRoot [32]byte, slowRelayRoot [32]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.RelayRootBundle(&_SpokePool.TransactOpts, relayerRefundRoot, slowRelayRoot)
}

// RelayRootBundle is a paid mutator transaction binding the contract method 0x493a4f84.
//
// Solidity: function relayRootBundle(bytes32 relayerRefundRoot, bytes32 slowRelayRoot) returns()
func (_SpokePool *SpokePoolTransactorSession) RelayRootBundle(relayerRefundRoot [32]byte, slowRelayRoot [32]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.RelayRootBundle(&_SpokePool.TransactOpts, relayerRefundRoot, slowRelayRoot)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_SpokePool *SpokePoolTransactor) RenounceOwnership(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "renounceOwnership")
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_SpokePool *SpokePoolSession) RenounceOwnership() (*types.Transaction, error) {
	return _SpokePool.Contract.RenounceOwnership(&_SpokePool.TransactOpts)
}

// RenounceOwnership is a paid mutator transaction binding the contract method 0x715018a6.
//
// Solidity: function renounceOwnership() returns()
func (_SpokePool *SpokePoolTransactorSession) RenounceOwnership() (*types.Transaction, error) {
	return _SpokePool.Contract.RenounceOwnership(&_SpokePool.TransactOpts)
}

// RequestSlowFill is a paid mutator transaction binding the contract method 0x2e63e59a.
//
// Solidity: function requestSlowFill((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes) relayData) returns()
func (_SpokePool *SpokePoolTransactor) RequestSlowFill(opts *bind.TransactOpts, relayData V3SpokePoolInterfaceV3RelayData) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "requestSlowFill", relayData)
}

// RequestSlowFill is a paid mutator transaction binding the contract method 0x2e63e59a.
//
// Solidity: function requestSlowFill((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes) relayData) returns()
func (_SpokePool *SpokePoolSession) RequestSlowFill(relayData V3SpokePoolInterfaceV3RelayData) (*types.Transaction, error) {
	return _SpokePool.Contract.RequestSlowFill(&_SpokePool.TransactOpts, relayData)
}

// RequestSlowFill is a paid mutator transaction binding the contract method 0x2e63e59a.
//
// Solidity: function requestSlowFill((bytes32,bytes32,bytes32,bytes32,bytes32,uint256,uint256,uint256,uint256,uint32,uint32,bytes) relayData) returns()
func (_SpokePool *SpokePoolTransactorSession) RequestSlowFill(relayData V3SpokePoolInterfaceV3RelayData) (*types.Transaction, error) {
	return _SpokePool.Contract.RequestSlowFill(&_SpokePool.TransactOpts, relayData)
}

// SetCrossDomainAdmin is a paid mutator transaction binding the contract method 0xde7eba78.
//
// Solidity: function setCrossDomainAdmin(address newCrossDomainAdmin) returns()
func (_SpokePool *SpokePoolTransactor) SetCrossDomainAdmin(opts *bind.TransactOpts, newCrossDomainAdmin common.Address) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "setCrossDomainAdmin", newCrossDomainAdmin)
}

// SetCrossDomainAdmin is a paid mutator transaction binding the contract method 0xde7eba78.
//
// Solidity: function setCrossDomainAdmin(address newCrossDomainAdmin) returns()
func (_SpokePool *SpokePoolSession) SetCrossDomainAdmin(newCrossDomainAdmin common.Address) (*types.Transaction, error) {
	return _SpokePool.Contract.SetCrossDomainAdmin(&_SpokePool.TransactOpts, newCrossDomainAdmin)
}

// SetCrossDomainAdmin is a paid mutator transaction binding the contract method 0xde7eba78.
//
// Solidity: function setCrossDomainAdmin(address newCrossDomainAdmin) returns()
func (_SpokePool *SpokePoolTransactorSession) SetCrossDomainAdmin(newCrossDomainAdmin common.Address) (*types.Transaction, error) {
	return _SpokePool.Contract.SetCrossDomainAdmin(&_SpokePool.TransactOpts, newCrossDomainAdmin)
}

// SetWithdrawalRecipient is a paid mutator transaction binding the contract method 0xfc8a584f.
//
// Solidity: function setWithdrawalRecipient(address newWithdrawalRecipient) returns()
func (_SpokePool *SpokePoolTransactor) SetWithdrawalRecipient(opts *bind.TransactOpts, newWithdrawalRecipient common.Address) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "setWithdrawalRecipient", newWithdrawalRecipient)
}

// SetWithdrawalRecipient is a paid mutator transaction binding the contract method 0xfc8a584f.
//
// Solidity: function setWithdrawalRecipient(address newWithdrawalRecipient) returns()
func (_SpokePool *SpokePoolSession) SetWithdrawalRecipient(newWithdrawalRecipient common.Address) (*types.Transaction, error) {
	return _SpokePool.Contract.SetWithdrawalRecipient(&_SpokePool.TransactOpts, newWithdrawalRecipient)
}

// SetWithdrawalRecipient is a paid mutator transaction binding the contract method 0xfc8a584f.
//
// Solidity: function setWithdrawalRecipient(address newWithdrawalRecipient) returns()
func (_SpokePool *SpokePoolTransactorSession) SetWithdrawalRecipient(newWithdrawalRecipient common.Address) (*types.Transaction, error) {
	return _SpokePool.Contract.SetWithdrawalRecipient(&_SpokePool.TransactOpts, newWithdrawalRecipient)
}

// SpeedUpDeposit is a paid mutator transaction binding the contract method 0xbabb6aac.
//
// Solidity: function speedUpDeposit(bytes32 depositor, uint256 depositId, uint256 updatedOutputAmount, bytes32 updatedRecipient, bytes updatedMessage, bytes depositorSignature) returns()
func (_SpokePool *SpokePoolTransactor) SpeedUpDeposit(opts *bind.TransactOpts, depositor [32]byte, depositId *big.Int, updatedOutputAmount *big.Int, updatedRecipient [32]byte, updatedMessage []byte, depositorSignature []byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "speedUpDeposit", depositor, depositId, updatedOutputAmount, updatedRecipient, updatedMessage, depositorSignature)
}

// SpeedUpDeposit is a paid mutator transaction binding the contract method 0xbabb6aac.
//
// Solidity: function speedUpDeposit(bytes32 depositor, uint256 depositId, uint256 updatedOutputAmount, bytes32 updatedRecipient, bytes updatedMessage, bytes depositorSignature) returns()
func (_SpokePool *SpokePoolSession) SpeedUpDeposit(depositor [32]byte, depositId *big.Int, updatedOutputAmount *big.Int, updatedRecipient [32]byte, updatedMessage []byte, depositorSignature []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.SpeedUpDeposit(&_SpokePool.TransactOpts, depositor, depositId, updatedOutputAmount, updatedRecipient, updatedMessage, depositorSignature)
}

// SpeedUpDeposit is a paid mutator transaction binding the contract method 0xbabb6aac.
//
// Solidity: function speedUpDeposit(bytes32 depositor, uint256 depositId, uint256 updatedOutputAmount, bytes32 updatedRecipient, bytes updatedMessage, bytes depositorSignature) returns()
func (_SpokePool *SpokePoolTransactorSession) SpeedUpDeposit(depositor [32]byte, depositId *big.Int, updatedOutputAmount *big.Int, updatedRecipient [32]byte, updatedMessage []byte, depositorSignature []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.SpeedUpDeposit(&_SpokePool.TransactOpts, depositor, depositId, updatedOutputAmount, updatedRecipient, updatedMessage, depositorSignature)
}

// SpeedUpV3Deposit is a paid mutator transaction binding the contract method 0x577f51f8.
//
// Solidity: function speedUpV3Deposit(address depositor, uint256 depositId, uint256 updatedOutputAmount, address updatedRecipient, bytes updatedMessage, bytes depositorSignature) returns()
func (_SpokePool *SpokePoolTransactor) SpeedUpV3Deposit(opts *bind.TransactOpts, depositor common.Address, depositId *big.Int, updatedOutputAmount *big.Int, updatedRecipient common.Address, updatedMessage []byte, depositorSignature []byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "speedUpV3Deposit", depositor, depositId, updatedOutputAmount, updatedRecipient, updatedMessage, depositorSignature)
}

// SpeedUpV3Deposit is a paid mutator transaction binding the contract method 0x577f51f8.
//
// Solidity: function speedUpV3Deposit(address depositor, uint256 depositId, uint256 updatedOutputAmount, address updatedRecipient, bytes updatedMessage, bytes depositorSignature) returns()
func (_SpokePool *SpokePoolSession) SpeedUpV3Deposit(depositor common.Address, depositId *big.Int, updatedOutputAmount *big.Int, updatedRecipient common.Address, updatedMessage []byte, depositorSignature []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.SpeedUpV3Deposit(&_SpokePool.TransactOpts, depositor, depositId, updatedOutputAmount, updatedRecipient, updatedMessage, depositorSignature)
}

// SpeedUpV3Deposit is a paid mutator transaction binding the contract method 0x577f51f8.
//
// Solidity: function speedUpV3Deposit(address depositor, uint256 depositId, uint256 updatedOutputAmount, address updatedRecipient, bytes updatedMessage, bytes depositorSignature) returns()
func (_SpokePool *SpokePoolTransactorSession) SpeedUpV3Deposit(depositor common.Address, depositId *big.Int, updatedOutputAmount *big.Int, updatedRecipient common.Address, updatedMessage []byte, depositorSignature []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.SpeedUpV3Deposit(&_SpokePool.TransactOpts, depositor, depositId, updatedOutputAmount, updatedRecipient, updatedMessage, depositorSignature)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_SpokePool *SpokePoolTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_SpokePool *SpokePoolSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _SpokePool.Contract.TransferOwnership(&_SpokePool.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_SpokePool *SpokePoolTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _SpokePool.Contract.TransferOwnership(&_SpokePool.TransactOpts, newOwner)
}

// TryMulticall is a paid mutator transaction binding the contract method 0x437b9116.
//
// Solidity: function tryMulticall(bytes[] data) returns((bool,bytes)[] results)
func (_SpokePool *SpokePoolTransactor) TryMulticall(opts *bind.TransactOpts, data [][]byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "tryMulticall", data)
}

// TryMulticall is a paid mutator transaction binding the contract method 0x437b9116.
//
// Solidity: function tryMulticall(bytes[] data) returns((bool,bytes)[] results)
func (_SpokePool *SpokePoolSession) TryMulticall(data [][]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.TryMulticall(&_SpokePool.TransactOpts, data)
}

// TryMulticall is a paid mutator transaction binding the contract method 0x437b9116.
//
// Solidity: function tryMulticall(bytes[] data) returns((bool,bytes)[] results)
func (_SpokePool *SpokePoolTransactorSession) TryMulticall(data [][]byte) (*types.Transaction, error) {
	return _SpokePool.Contract.TryMulticall(&_SpokePool.TransactOpts, data)
}

// UnsafeDeposit is a paid mutator transaction binding the contract method 0x8b15788e.
//
// Solidity: function unsafeDeposit(bytes32 depositor, bytes32 recipient, bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, bytes32 exclusiveRelayer, uint256 depositNonce, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolTransactor) UnsafeDeposit(opts *bind.TransactOpts, depositor [32]byte, recipient [32]byte, inputToken [32]byte, outputToken [32]byte, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer [32]byte, depositNonce *big.Int, quoteTimestamp uint32, fillDeadline uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "unsafeDeposit", depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, depositNonce, quoteTimestamp, fillDeadline, exclusivityParameter, message)
}

// UnsafeDeposit is a paid mutator transaction binding the contract method 0x8b15788e.
//
// Solidity: function unsafeDeposit(bytes32 depositor, bytes32 recipient, bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, bytes32 exclusiveRelayer, uint256 depositNonce, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolSession) UnsafeDeposit(depositor [32]byte, recipient [32]byte, inputToken [32]byte, outputToken [32]byte, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer [32]byte, depositNonce *big.Int, quoteTimestamp uint32, fillDeadline uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.UnsafeDeposit(&_SpokePool.TransactOpts, depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, depositNonce, quoteTimestamp, fillDeadline, exclusivityParameter, message)
}

// UnsafeDeposit is a paid mutator transaction binding the contract method 0x8b15788e.
//
// Solidity: function unsafeDeposit(bytes32 depositor, bytes32 recipient, bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 destinationChainId, bytes32 exclusiveRelayer, uint256 depositNonce, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityParameter, bytes message) payable returns()
func (_SpokePool *SpokePoolTransactorSession) UnsafeDeposit(depositor [32]byte, recipient [32]byte, inputToken [32]byte, outputToken [32]byte, inputAmount *big.Int, outputAmount *big.Int, destinationChainId *big.Int, exclusiveRelayer [32]byte, depositNonce *big.Int, quoteTimestamp uint32, fillDeadline uint32, exclusivityParameter uint32, message []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.UnsafeDeposit(&_SpokePool.TransactOpts, depositor, recipient, inputToken, outputToken, inputAmount, outputAmount, destinationChainId, exclusiveRelayer, depositNonce, quoteTimestamp, fillDeadline, exclusivityParameter, message)
}

// UpgradeTo is a paid mutator transaction binding the contract method 0x3659cfe6.
//
// Solidity: function upgradeTo(address newImplementation) returns()
func (_SpokePool *SpokePoolTransactor) UpgradeTo(opts *bind.TransactOpts, newImplementation common.Address) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "upgradeTo", newImplementation)
}

// UpgradeTo is a paid mutator transaction binding the contract method 0x3659cfe6.
//
// Solidity: function upgradeTo(address newImplementation) returns()
func (_SpokePool *SpokePoolSession) UpgradeTo(newImplementation common.Address) (*types.Transaction, error) {
	return _SpokePool.Contract.UpgradeTo(&_SpokePool.TransactOpts, newImplementation)
}

// UpgradeTo is a paid mutator transaction binding the contract method 0x3659cfe6.
//
// Solidity: function upgradeTo(address newImplementation) returns()
func (_SpokePool *SpokePoolTransactorSession) UpgradeTo(newImplementation common.Address) (*types.Transaction, error) {
	return _SpokePool.Contract.UpgradeTo(&_SpokePool.TransactOpts, newImplementation)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_SpokePool *SpokePoolTransactor) UpgradeToAndCall(opts *bind.TransactOpts, newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _SpokePool.contract.Transact(opts, "upgradeToAndCall", newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_SpokePool *SpokePoolSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.UpgradeToAndCall(&_SpokePool.TransactOpts, newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_SpokePool *SpokePoolTransactorSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _SpokePool.Contract.UpgradeToAndCall(&_SpokePool.TransactOpts, newImplementation, data)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_SpokePool *SpokePoolTransactor) Receive(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _SpokePool.contract.RawTransact(opts, nil) // calldata is disallowed for receive function
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_SpokePool *SpokePoolSession) Receive() (*types.Transaction, error) {
	return _SpokePool.Contract.Receive(&_SpokePool.TransactOpts)
}

// Receive is a paid mutator transaction binding the contract receive function.
//
// Solidity: receive() payable returns()
func (_SpokePool *SpokePoolTransactorSession) Receive() (*types.Transaction, error) {
	return _SpokePool.Contract.Receive(&_SpokePool.TransactOpts)
}

// SpokePoolAdminChangedIterator is returned from FilterAdminChanged and is used to iterate over the raw logs and unpacked data for AdminChanged events raised by the SpokePool contract.
type SpokePoolAdminChangedIterator struct {
	Event *SpokePoolAdminChanged // Event containing the contract specifics and raw log

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
func (it *SpokePoolAdminChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolAdminChanged)
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
		it.Event = new(SpokePoolAdminChanged)
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
func (it *SpokePoolAdminChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolAdminChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolAdminChanged represents a AdminChanged event raised by the SpokePool contract.
type SpokePoolAdminChanged struct {
	PreviousAdmin common.Address
	NewAdmin      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterAdminChanged is a free log retrieval operation binding the contract event 0x7e644d79422f17c01e4894b5f4f588d331ebfa28653d42ae832dc59e38c9798f.
//
// Solidity: event AdminChanged(address previousAdmin, address newAdmin)
func (_SpokePool *SpokePoolFilterer) FilterAdminChanged(opts *bind.FilterOpts) (*SpokePoolAdminChangedIterator, error) {

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "AdminChanged")
	if err != nil {
		return nil, err
	}
	return &SpokePoolAdminChangedIterator{contract: _SpokePool.contract, event: "AdminChanged", logs: logs, sub: sub}, nil
}

// WatchAdminChanged is a free log subscription operation binding the contract event 0x7e644d79422f17c01e4894b5f4f588d331ebfa28653d42ae832dc59e38c9798f.
//
// Solidity: event AdminChanged(address previousAdmin, address newAdmin)
func (_SpokePool *SpokePoolFilterer) WatchAdminChanged(opts *bind.WatchOpts, sink chan<- *SpokePoolAdminChanged) (event.Subscription, error) {

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "AdminChanged")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolAdminChanged)
				if err := _SpokePool.contract.UnpackLog(event, "AdminChanged", log); err != nil {
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

// ParseAdminChanged is a log parse operation binding the contract event 0x7e644d79422f17c01e4894b5f4f588d331ebfa28653d42ae832dc59e38c9798f.
//
// Solidity: event AdminChanged(address previousAdmin, address newAdmin)
func (_SpokePool *SpokePoolFilterer) ParseAdminChanged(log types.Log) (*SpokePoolAdminChanged, error) {
	event := new(SpokePoolAdminChanged)
	if err := _SpokePool.contract.UnpackLog(event, "AdminChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolBeaconUpgradedIterator is returned from FilterBeaconUpgraded and is used to iterate over the raw logs and unpacked data for BeaconUpgraded events raised by the SpokePool contract.
type SpokePoolBeaconUpgradedIterator struct {
	Event *SpokePoolBeaconUpgraded // Event containing the contract specifics and raw log

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
func (it *SpokePoolBeaconUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolBeaconUpgraded)
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
		it.Event = new(SpokePoolBeaconUpgraded)
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
func (it *SpokePoolBeaconUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolBeaconUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolBeaconUpgraded represents a BeaconUpgraded event raised by the SpokePool contract.
type SpokePoolBeaconUpgraded struct {
	Beacon common.Address
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterBeaconUpgraded is a free log retrieval operation binding the contract event 0x1cf3b03a6cf19fa2baba4df148e9dcabedea7f8a5c07840e207e5c089be95d3e.
//
// Solidity: event BeaconUpgraded(address indexed beacon)
func (_SpokePool *SpokePoolFilterer) FilterBeaconUpgraded(opts *bind.FilterOpts, beacon []common.Address) (*SpokePoolBeaconUpgradedIterator, error) {

	var beaconRule []interface{}
	for _, beaconItem := range beacon {
		beaconRule = append(beaconRule, beaconItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "BeaconUpgraded", beaconRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolBeaconUpgradedIterator{contract: _SpokePool.contract, event: "BeaconUpgraded", logs: logs, sub: sub}, nil
}

// WatchBeaconUpgraded is a free log subscription operation binding the contract event 0x1cf3b03a6cf19fa2baba4df148e9dcabedea7f8a5c07840e207e5c089be95d3e.
//
// Solidity: event BeaconUpgraded(address indexed beacon)
func (_SpokePool *SpokePoolFilterer) WatchBeaconUpgraded(opts *bind.WatchOpts, sink chan<- *SpokePoolBeaconUpgraded, beacon []common.Address) (event.Subscription, error) {

	var beaconRule []interface{}
	for _, beaconItem := range beacon {
		beaconRule = append(beaconRule, beaconItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "BeaconUpgraded", beaconRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolBeaconUpgraded)
				if err := _SpokePool.contract.UnpackLog(event, "BeaconUpgraded", log); err != nil {
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

// ParseBeaconUpgraded is a log parse operation binding the contract event 0x1cf3b03a6cf19fa2baba4df148e9dcabedea7f8a5c07840e207e5c089be95d3e.
//
// Solidity: event BeaconUpgraded(address indexed beacon)
func (_SpokePool *SpokePoolFilterer) ParseBeaconUpgraded(log types.Log) (*SpokePoolBeaconUpgraded, error) {
	event := new(SpokePoolBeaconUpgraded)
	if err := _SpokePool.contract.UnpackLog(event, "BeaconUpgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolClaimedRelayerRefundIterator is returned from FilterClaimedRelayerRefund and is used to iterate over the raw logs and unpacked data for ClaimedRelayerRefund events raised by the SpokePool contract.
type SpokePoolClaimedRelayerRefundIterator struct {
	Event *SpokePoolClaimedRelayerRefund // Event containing the contract specifics and raw log

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
func (it *SpokePoolClaimedRelayerRefundIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolClaimedRelayerRefund)
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
		it.Event = new(SpokePoolClaimedRelayerRefund)
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
func (it *SpokePoolClaimedRelayerRefundIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolClaimedRelayerRefundIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolClaimedRelayerRefund represents a ClaimedRelayerRefund event raised by the SpokePool contract.
type SpokePoolClaimedRelayerRefund struct {
	L2TokenAddress [32]byte
	RefundAddress  [32]byte
	Amount         *big.Int
	Caller         common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterClaimedRelayerRefund is a free log retrieval operation binding the contract event 0x6c172ea51018fb2eb2118f3f8a507c4df71eb519b8c0052834dc3c920182fef4.
//
// Solidity: event ClaimedRelayerRefund(bytes32 indexed l2TokenAddress, bytes32 indexed refundAddress, uint256 amount, address indexed caller)
func (_SpokePool *SpokePoolFilterer) FilterClaimedRelayerRefund(opts *bind.FilterOpts, l2TokenAddress [][32]byte, refundAddress [][32]byte, caller []common.Address) (*SpokePoolClaimedRelayerRefundIterator, error) {

	var l2TokenAddressRule []interface{}
	for _, l2TokenAddressItem := range l2TokenAddress {
		l2TokenAddressRule = append(l2TokenAddressRule, l2TokenAddressItem)
	}
	var refundAddressRule []interface{}
	for _, refundAddressItem := range refundAddress {
		refundAddressRule = append(refundAddressRule, refundAddressItem)
	}

	var callerRule []interface{}
	for _, callerItem := range caller {
		callerRule = append(callerRule, callerItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "ClaimedRelayerRefund", l2TokenAddressRule, refundAddressRule, callerRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolClaimedRelayerRefundIterator{contract: _SpokePool.contract, event: "ClaimedRelayerRefund", logs: logs, sub: sub}, nil
}

// WatchClaimedRelayerRefund is a free log subscription operation binding the contract event 0x6c172ea51018fb2eb2118f3f8a507c4df71eb519b8c0052834dc3c920182fef4.
//
// Solidity: event ClaimedRelayerRefund(bytes32 indexed l2TokenAddress, bytes32 indexed refundAddress, uint256 amount, address indexed caller)
func (_SpokePool *SpokePoolFilterer) WatchClaimedRelayerRefund(opts *bind.WatchOpts, sink chan<- *SpokePoolClaimedRelayerRefund, l2TokenAddress [][32]byte, refundAddress [][32]byte, caller []common.Address) (event.Subscription, error) {

	var l2TokenAddressRule []interface{}
	for _, l2TokenAddressItem := range l2TokenAddress {
		l2TokenAddressRule = append(l2TokenAddressRule, l2TokenAddressItem)
	}
	var refundAddressRule []interface{}
	for _, refundAddressItem := range refundAddress {
		refundAddressRule = append(refundAddressRule, refundAddressItem)
	}

	var callerRule []interface{}
	for _, callerItem := range caller {
		callerRule = append(callerRule, callerItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "ClaimedRelayerRefund", l2TokenAddressRule, refundAddressRule, callerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolClaimedRelayerRefund)
				if err := _SpokePool.contract.UnpackLog(event, "ClaimedRelayerRefund", log); err != nil {
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

// ParseClaimedRelayerRefund is a log parse operation binding the contract event 0x6c172ea51018fb2eb2118f3f8a507c4df71eb519b8c0052834dc3c920182fef4.
//
// Solidity: event ClaimedRelayerRefund(bytes32 indexed l2TokenAddress, bytes32 indexed refundAddress, uint256 amount, address indexed caller)
func (_SpokePool *SpokePoolFilterer) ParseClaimedRelayerRefund(log types.Log) (*SpokePoolClaimedRelayerRefund, error) {
	event := new(SpokePoolClaimedRelayerRefund)
	if err := _SpokePool.contract.UnpackLog(event, "ClaimedRelayerRefund", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolEmergencyDeletedRootBundleIterator is returned from FilterEmergencyDeletedRootBundle and is used to iterate over the raw logs and unpacked data for EmergencyDeletedRootBundle events raised by the SpokePool contract.
type SpokePoolEmergencyDeletedRootBundleIterator struct {
	Event *SpokePoolEmergencyDeletedRootBundle // Event containing the contract specifics and raw log

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
func (it *SpokePoolEmergencyDeletedRootBundleIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolEmergencyDeletedRootBundle)
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
		it.Event = new(SpokePoolEmergencyDeletedRootBundle)
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
func (it *SpokePoolEmergencyDeletedRootBundleIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolEmergencyDeletedRootBundleIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolEmergencyDeletedRootBundle represents a EmergencyDeletedRootBundle event raised by the SpokePool contract.
type SpokePoolEmergencyDeletedRootBundle struct {
	RootBundleId *big.Int
	Raw          types.Log // Blockchain specific contextual infos
}

// FilterEmergencyDeletedRootBundle is a free log retrieval operation binding the contract event 0x7c1af0646963afc3343245b103731965735a893347bfa0d58a5dc77a77ae691c.
//
// Solidity: event EmergencyDeletedRootBundle(uint256 indexed rootBundleId)
func (_SpokePool *SpokePoolFilterer) FilterEmergencyDeletedRootBundle(opts *bind.FilterOpts, rootBundleId []*big.Int) (*SpokePoolEmergencyDeletedRootBundleIterator, error) {

	var rootBundleIdRule []interface{}
	for _, rootBundleIdItem := range rootBundleId {
		rootBundleIdRule = append(rootBundleIdRule, rootBundleIdItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "EmergencyDeletedRootBundle", rootBundleIdRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolEmergencyDeletedRootBundleIterator{contract: _SpokePool.contract, event: "EmergencyDeletedRootBundle", logs: logs, sub: sub}, nil
}

// WatchEmergencyDeletedRootBundle is a free log subscription operation binding the contract event 0x7c1af0646963afc3343245b103731965735a893347bfa0d58a5dc77a77ae691c.
//
// Solidity: event EmergencyDeletedRootBundle(uint256 indexed rootBundleId)
func (_SpokePool *SpokePoolFilterer) WatchEmergencyDeletedRootBundle(opts *bind.WatchOpts, sink chan<- *SpokePoolEmergencyDeletedRootBundle, rootBundleId []*big.Int) (event.Subscription, error) {

	var rootBundleIdRule []interface{}
	for _, rootBundleIdItem := range rootBundleId {
		rootBundleIdRule = append(rootBundleIdRule, rootBundleIdItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "EmergencyDeletedRootBundle", rootBundleIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolEmergencyDeletedRootBundle)
				if err := _SpokePool.contract.UnpackLog(event, "EmergencyDeletedRootBundle", log); err != nil {
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

// ParseEmergencyDeletedRootBundle is a log parse operation binding the contract event 0x7c1af0646963afc3343245b103731965735a893347bfa0d58a5dc77a77ae691c.
//
// Solidity: event EmergencyDeletedRootBundle(uint256 indexed rootBundleId)
func (_SpokePool *SpokePoolFilterer) ParseEmergencyDeletedRootBundle(log types.Log) (*SpokePoolEmergencyDeletedRootBundle, error) {
	event := new(SpokePoolEmergencyDeletedRootBundle)
	if err := _SpokePool.contract.UnpackLog(event, "EmergencyDeletedRootBundle", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolEnabledDepositRouteIterator is returned from FilterEnabledDepositRoute and is used to iterate over the raw logs and unpacked data for EnabledDepositRoute events raised by the SpokePool contract.
type SpokePoolEnabledDepositRouteIterator struct {
	Event *SpokePoolEnabledDepositRoute // Event containing the contract specifics and raw log

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
func (it *SpokePoolEnabledDepositRouteIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolEnabledDepositRoute)
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
		it.Event = new(SpokePoolEnabledDepositRoute)
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
func (it *SpokePoolEnabledDepositRouteIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolEnabledDepositRouteIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolEnabledDepositRoute represents a EnabledDepositRoute event raised by the SpokePool contract.
type SpokePoolEnabledDepositRoute struct {
	OriginToken        common.Address
	DestinationChainId *big.Int
	Enabled            bool
	Raw                types.Log // Blockchain specific contextual infos
}

// FilterEnabledDepositRoute is a free log retrieval operation binding the contract event 0x0a21fdd43d0ad0c62689ee7230a47309a050755bcc52eba00310add65297692a.
//
// Solidity: event EnabledDepositRoute(address indexed originToken, uint256 indexed destinationChainId, bool enabled)
func (_SpokePool *SpokePoolFilterer) FilterEnabledDepositRoute(opts *bind.FilterOpts, originToken []common.Address, destinationChainId []*big.Int) (*SpokePoolEnabledDepositRouteIterator, error) {

	var originTokenRule []interface{}
	for _, originTokenItem := range originToken {
		originTokenRule = append(originTokenRule, originTokenItem)
	}
	var destinationChainIdRule []interface{}
	for _, destinationChainIdItem := range destinationChainId {
		destinationChainIdRule = append(destinationChainIdRule, destinationChainIdItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "EnabledDepositRoute", originTokenRule, destinationChainIdRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolEnabledDepositRouteIterator{contract: _SpokePool.contract, event: "EnabledDepositRoute", logs: logs, sub: sub}, nil
}

// WatchEnabledDepositRoute is a free log subscription operation binding the contract event 0x0a21fdd43d0ad0c62689ee7230a47309a050755bcc52eba00310add65297692a.
//
// Solidity: event EnabledDepositRoute(address indexed originToken, uint256 indexed destinationChainId, bool enabled)
func (_SpokePool *SpokePoolFilterer) WatchEnabledDepositRoute(opts *bind.WatchOpts, sink chan<- *SpokePoolEnabledDepositRoute, originToken []common.Address, destinationChainId []*big.Int) (event.Subscription, error) {

	var originTokenRule []interface{}
	for _, originTokenItem := range originToken {
		originTokenRule = append(originTokenRule, originTokenItem)
	}
	var destinationChainIdRule []interface{}
	for _, destinationChainIdItem := range destinationChainId {
		destinationChainIdRule = append(destinationChainIdRule, destinationChainIdItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "EnabledDepositRoute", originTokenRule, destinationChainIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolEnabledDepositRoute)
				if err := _SpokePool.contract.UnpackLog(event, "EnabledDepositRoute", log); err != nil {
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

// ParseEnabledDepositRoute is a log parse operation binding the contract event 0x0a21fdd43d0ad0c62689ee7230a47309a050755bcc52eba00310add65297692a.
//
// Solidity: event EnabledDepositRoute(address indexed originToken, uint256 indexed destinationChainId, bool enabled)
func (_SpokePool *SpokePoolFilterer) ParseEnabledDepositRoute(log types.Log) (*SpokePoolEnabledDepositRoute, error) {
	event := new(SpokePoolEnabledDepositRoute)
	if err := _SpokePool.contract.UnpackLog(event, "EnabledDepositRoute", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolExecutedRelayerRefundRootIterator is returned from FilterExecutedRelayerRefundRoot and is used to iterate over the raw logs and unpacked data for ExecutedRelayerRefundRoot events raised by the SpokePool contract.
type SpokePoolExecutedRelayerRefundRootIterator struct {
	Event *SpokePoolExecutedRelayerRefundRoot // Event containing the contract specifics and raw log

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
func (it *SpokePoolExecutedRelayerRefundRootIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolExecutedRelayerRefundRoot)
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
		it.Event = new(SpokePoolExecutedRelayerRefundRoot)
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
func (it *SpokePoolExecutedRelayerRefundRootIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolExecutedRelayerRefundRootIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolExecutedRelayerRefundRoot represents a ExecutedRelayerRefundRoot event raised by the SpokePool contract.
type SpokePoolExecutedRelayerRefundRoot struct {
	AmountToReturn  *big.Int
	ChainId         *big.Int
	RefundAmounts   []*big.Int
	RootBundleId    uint32
	LeafId          uint32
	L2TokenAddress  common.Address
	RefundAddresses []common.Address
	DeferredRefunds bool
	Caller          common.Address
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterExecutedRelayerRefundRoot is a free log retrieval operation binding the contract event 0xf4ad92585b1bc117fbdd644990adf0827bc4c95baeae8a23322af807b6d0020e.
//
// Solidity: event ExecutedRelayerRefundRoot(uint256 amountToReturn, uint256 indexed chainId, uint256[] refundAmounts, uint32 indexed rootBundleId, uint32 indexed leafId, address l2TokenAddress, address[] refundAddresses, bool deferredRefunds, address caller)
func (_SpokePool *SpokePoolFilterer) FilterExecutedRelayerRefundRoot(opts *bind.FilterOpts, chainId []*big.Int, rootBundleId []uint32, leafId []uint32) (*SpokePoolExecutedRelayerRefundRootIterator, error) {

	var chainIdRule []interface{}
	for _, chainIdItem := range chainId {
		chainIdRule = append(chainIdRule, chainIdItem)
	}

	var rootBundleIdRule []interface{}
	for _, rootBundleIdItem := range rootBundleId {
		rootBundleIdRule = append(rootBundleIdRule, rootBundleIdItem)
	}
	var leafIdRule []interface{}
	for _, leafIdItem := range leafId {
		leafIdRule = append(leafIdRule, leafIdItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "ExecutedRelayerRefundRoot", chainIdRule, rootBundleIdRule, leafIdRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolExecutedRelayerRefundRootIterator{contract: _SpokePool.contract, event: "ExecutedRelayerRefundRoot", logs: logs, sub: sub}, nil
}

// WatchExecutedRelayerRefundRoot is a free log subscription operation binding the contract event 0xf4ad92585b1bc117fbdd644990adf0827bc4c95baeae8a23322af807b6d0020e.
//
// Solidity: event ExecutedRelayerRefundRoot(uint256 amountToReturn, uint256 indexed chainId, uint256[] refundAmounts, uint32 indexed rootBundleId, uint32 indexed leafId, address l2TokenAddress, address[] refundAddresses, bool deferredRefunds, address caller)
func (_SpokePool *SpokePoolFilterer) WatchExecutedRelayerRefundRoot(opts *bind.WatchOpts, sink chan<- *SpokePoolExecutedRelayerRefundRoot, chainId []*big.Int, rootBundleId []uint32, leafId []uint32) (event.Subscription, error) {

	var chainIdRule []interface{}
	for _, chainIdItem := range chainId {
		chainIdRule = append(chainIdRule, chainIdItem)
	}

	var rootBundleIdRule []interface{}
	for _, rootBundleIdItem := range rootBundleId {
		rootBundleIdRule = append(rootBundleIdRule, rootBundleIdItem)
	}
	var leafIdRule []interface{}
	for _, leafIdItem := range leafId {
		leafIdRule = append(leafIdRule, leafIdItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "ExecutedRelayerRefundRoot", chainIdRule, rootBundleIdRule, leafIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolExecutedRelayerRefundRoot)
				if err := _SpokePool.contract.UnpackLog(event, "ExecutedRelayerRefundRoot", log); err != nil {
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

// ParseExecutedRelayerRefundRoot is a log parse operation binding the contract event 0xf4ad92585b1bc117fbdd644990adf0827bc4c95baeae8a23322af807b6d0020e.
//
// Solidity: event ExecutedRelayerRefundRoot(uint256 amountToReturn, uint256 indexed chainId, uint256[] refundAmounts, uint32 indexed rootBundleId, uint32 indexed leafId, address l2TokenAddress, address[] refundAddresses, bool deferredRefunds, address caller)
func (_SpokePool *SpokePoolFilterer) ParseExecutedRelayerRefundRoot(log types.Log) (*SpokePoolExecutedRelayerRefundRoot, error) {
	event := new(SpokePoolExecutedRelayerRefundRoot)
	if err := _SpokePool.contract.UnpackLog(event, "ExecutedRelayerRefundRoot", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolFilledRelayIterator is returned from FilterFilledRelay and is used to iterate over the raw logs and unpacked data for FilledRelay events raised by the SpokePool contract.
type SpokePoolFilledRelayIterator struct {
	Event *SpokePoolFilledRelay // Event containing the contract specifics and raw log

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
func (it *SpokePoolFilledRelayIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolFilledRelay)
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
		it.Event = new(SpokePoolFilledRelay)
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
func (it *SpokePoolFilledRelayIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolFilledRelayIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolFilledRelay represents a FilledRelay event raised by the SpokePool contract.
type SpokePoolFilledRelay struct {
	InputToken          [32]byte
	OutputToken         [32]byte
	InputAmount         *big.Int
	OutputAmount        *big.Int
	RepaymentChainId    *big.Int
	OriginChainId       *big.Int
	DepositId           *big.Int
	FillDeadline        uint32
	ExclusivityDeadline uint32
	ExclusiveRelayer    [32]byte
	Relayer             [32]byte
	Depositor           [32]byte
	Recipient           [32]byte
	MessageHash         [32]byte
	RelayExecutionInfo  V3SpokePoolInterfaceV3RelayExecutionEventInfo
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterFilledRelay is a free log retrieval operation binding the contract event 0x44b559f101f8fbcc8a0ea43fa91a05a729a5ea6e14a7c75aa750374690137208.
//
// Solidity: event FilledRelay(bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 repaymentChainId, uint256 indexed originChainId, uint256 indexed depositId, uint32 fillDeadline, uint32 exclusivityDeadline, bytes32 exclusiveRelayer, bytes32 indexed relayer, bytes32 depositor, bytes32 recipient, bytes32 messageHash, (bytes32,bytes32,uint256,uint8) relayExecutionInfo)
func (_SpokePool *SpokePoolFilterer) FilterFilledRelay(opts *bind.FilterOpts, originChainId []*big.Int, depositId []*big.Int, relayer [][32]byte) (*SpokePoolFilledRelayIterator, error) {

	var originChainIdRule []interface{}
	for _, originChainIdItem := range originChainId {
		originChainIdRule = append(originChainIdRule, originChainIdItem)
	}
	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}

	var relayerRule []interface{}
	for _, relayerItem := range relayer {
		relayerRule = append(relayerRule, relayerItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "FilledRelay", originChainIdRule, depositIdRule, relayerRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolFilledRelayIterator{contract: _SpokePool.contract, event: "FilledRelay", logs: logs, sub: sub}, nil
}

// WatchFilledRelay is a free log subscription operation binding the contract event 0x44b559f101f8fbcc8a0ea43fa91a05a729a5ea6e14a7c75aa750374690137208.
//
// Solidity: event FilledRelay(bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 repaymentChainId, uint256 indexed originChainId, uint256 indexed depositId, uint32 fillDeadline, uint32 exclusivityDeadline, bytes32 exclusiveRelayer, bytes32 indexed relayer, bytes32 depositor, bytes32 recipient, bytes32 messageHash, (bytes32,bytes32,uint256,uint8) relayExecutionInfo)
func (_SpokePool *SpokePoolFilterer) WatchFilledRelay(opts *bind.WatchOpts, sink chan<- *SpokePoolFilledRelay, originChainId []*big.Int, depositId []*big.Int, relayer [][32]byte) (event.Subscription, error) {

	var originChainIdRule []interface{}
	for _, originChainIdItem := range originChainId {
		originChainIdRule = append(originChainIdRule, originChainIdItem)
	}
	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}

	var relayerRule []interface{}
	for _, relayerItem := range relayer {
		relayerRule = append(relayerRule, relayerItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "FilledRelay", originChainIdRule, depositIdRule, relayerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolFilledRelay)
				if err := _SpokePool.contract.UnpackLog(event, "FilledRelay", log); err != nil {
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

// ParseFilledRelay is a log parse operation binding the contract event 0x44b559f101f8fbcc8a0ea43fa91a05a729a5ea6e14a7c75aa750374690137208.
//
// Solidity: event FilledRelay(bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 repaymentChainId, uint256 indexed originChainId, uint256 indexed depositId, uint32 fillDeadline, uint32 exclusivityDeadline, bytes32 exclusiveRelayer, bytes32 indexed relayer, bytes32 depositor, bytes32 recipient, bytes32 messageHash, (bytes32,bytes32,uint256,uint8) relayExecutionInfo)
func (_SpokePool *SpokePoolFilterer) ParseFilledRelay(log types.Log) (*SpokePoolFilledRelay, error) {
	event := new(SpokePoolFilledRelay)
	if err := _SpokePool.contract.UnpackLog(event, "FilledRelay", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolFilledV3RelayIterator is returned from FilterFilledV3Relay and is used to iterate over the raw logs and unpacked data for FilledV3Relay events raised by the SpokePool contract.
type SpokePoolFilledV3RelayIterator struct {
	Event *SpokePoolFilledV3Relay // Event containing the contract specifics and raw log

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
func (it *SpokePoolFilledV3RelayIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolFilledV3Relay)
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
		it.Event = new(SpokePoolFilledV3Relay)
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
func (it *SpokePoolFilledV3RelayIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolFilledV3RelayIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolFilledV3Relay represents a FilledV3Relay event raised by the SpokePool contract.
type SpokePoolFilledV3Relay struct {
	InputToken          common.Address
	OutputToken         common.Address
	InputAmount         *big.Int
	OutputAmount        *big.Int
	RepaymentChainId    *big.Int
	OriginChainId       *big.Int
	DepositId           uint32
	FillDeadline        uint32
	ExclusivityDeadline uint32
	ExclusiveRelayer    common.Address
	Relayer             common.Address
	Depositor           common.Address
	Recipient           common.Address
	Message             []byte
	RelayExecutionInfo  V3SpokePoolInterfaceLegacyV3RelayExecutionEventInfo
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterFilledV3Relay is a free log retrieval operation binding the contract event 0x571749edf1d5c9599318cdbc4e28a6475d65e87fd3b2ddbe1e9a8d5e7a0f0ff7.
//
// Solidity: event FilledV3Relay(address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 repaymentChainId, uint256 indexed originChainId, uint32 indexed depositId, uint32 fillDeadline, uint32 exclusivityDeadline, address exclusiveRelayer, address indexed relayer, address depositor, address recipient, bytes message, (address,bytes,uint256,uint8) relayExecutionInfo)
func (_SpokePool *SpokePoolFilterer) FilterFilledV3Relay(opts *bind.FilterOpts, originChainId []*big.Int, depositId []uint32, relayer []common.Address) (*SpokePoolFilledV3RelayIterator, error) {

	var originChainIdRule []interface{}
	for _, originChainIdItem := range originChainId {
		originChainIdRule = append(originChainIdRule, originChainIdItem)
	}
	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}

	var relayerRule []interface{}
	for _, relayerItem := range relayer {
		relayerRule = append(relayerRule, relayerItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "FilledV3Relay", originChainIdRule, depositIdRule, relayerRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolFilledV3RelayIterator{contract: _SpokePool.contract, event: "FilledV3Relay", logs: logs, sub: sub}, nil
}

// WatchFilledV3Relay is a free log subscription operation binding the contract event 0x571749edf1d5c9599318cdbc4e28a6475d65e87fd3b2ddbe1e9a8d5e7a0f0ff7.
//
// Solidity: event FilledV3Relay(address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 repaymentChainId, uint256 indexed originChainId, uint32 indexed depositId, uint32 fillDeadline, uint32 exclusivityDeadline, address exclusiveRelayer, address indexed relayer, address depositor, address recipient, bytes message, (address,bytes,uint256,uint8) relayExecutionInfo)
func (_SpokePool *SpokePoolFilterer) WatchFilledV3Relay(opts *bind.WatchOpts, sink chan<- *SpokePoolFilledV3Relay, originChainId []*big.Int, depositId []uint32, relayer []common.Address) (event.Subscription, error) {

	var originChainIdRule []interface{}
	for _, originChainIdItem := range originChainId {
		originChainIdRule = append(originChainIdRule, originChainIdItem)
	}
	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}

	var relayerRule []interface{}
	for _, relayerItem := range relayer {
		relayerRule = append(relayerRule, relayerItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "FilledV3Relay", originChainIdRule, depositIdRule, relayerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolFilledV3Relay)
				if err := _SpokePool.contract.UnpackLog(event, "FilledV3Relay", log); err != nil {
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

// ParseFilledV3Relay is a log parse operation binding the contract event 0x571749edf1d5c9599318cdbc4e28a6475d65e87fd3b2ddbe1e9a8d5e7a0f0ff7.
//
// Solidity: event FilledV3Relay(address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 repaymentChainId, uint256 indexed originChainId, uint32 indexed depositId, uint32 fillDeadline, uint32 exclusivityDeadline, address exclusiveRelayer, address indexed relayer, address depositor, address recipient, bytes message, (address,bytes,uint256,uint8) relayExecutionInfo)
func (_SpokePool *SpokePoolFilterer) ParseFilledV3Relay(log types.Log) (*SpokePoolFilledV3Relay, error) {
	event := new(SpokePoolFilledV3Relay)
	if err := _SpokePool.contract.UnpackLog(event, "FilledV3Relay", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolFundsDepositedIterator is returned from FilterFundsDeposited and is used to iterate over the raw logs and unpacked data for FundsDeposited events raised by the SpokePool contract.
type SpokePoolFundsDepositedIterator struct {
	Event *SpokePoolFundsDeposited // Event containing the contract specifics and raw log

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
func (it *SpokePoolFundsDepositedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolFundsDeposited)
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
		it.Event = new(SpokePoolFundsDeposited)
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
func (it *SpokePoolFundsDepositedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolFundsDepositedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolFundsDeposited represents a FundsDeposited event raised by the SpokePool contract.
type SpokePoolFundsDeposited struct {
	InputToken          [32]byte
	OutputToken         [32]byte
	InputAmount         *big.Int
	OutputAmount        *big.Int
	DestinationChainId  *big.Int
	DepositId           *big.Int
	QuoteTimestamp      uint32
	FillDeadline        uint32
	ExclusivityDeadline uint32
	Depositor           [32]byte
	Recipient           [32]byte
	ExclusiveRelayer    [32]byte
	Message             []byte
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterFundsDeposited is a free log retrieval operation binding the contract event 0x32ed1a409ef04c7b0227189c3a103dc5ac10e775a15b785dcc510201f7c25ad3.
//
// Solidity: event FundsDeposited(bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 indexed destinationChainId, uint256 indexed depositId, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityDeadline, bytes32 indexed depositor, bytes32 recipient, bytes32 exclusiveRelayer, bytes message)
func (_SpokePool *SpokePoolFilterer) FilterFundsDeposited(opts *bind.FilterOpts, destinationChainId []*big.Int, depositId []*big.Int, depositor [][32]byte) (*SpokePoolFundsDepositedIterator, error) {

	var destinationChainIdRule []interface{}
	for _, destinationChainIdItem := range destinationChainId {
		destinationChainIdRule = append(destinationChainIdRule, destinationChainIdItem)
	}
	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}

	var depositorRule []interface{}
	for _, depositorItem := range depositor {
		depositorRule = append(depositorRule, depositorItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "FundsDeposited", destinationChainIdRule, depositIdRule, depositorRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolFundsDepositedIterator{contract: _SpokePool.contract, event: "FundsDeposited", logs: logs, sub: sub}, nil
}

// WatchFundsDeposited is a free log subscription operation binding the contract event 0x32ed1a409ef04c7b0227189c3a103dc5ac10e775a15b785dcc510201f7c25ad3.
//
// Solidity: event FundsDeposited(bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 indexed destinationChainId, uint256 indexed depositId, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityDeadline, bytes32 indexed depositor, bytes32 recipient, bytes32 exclusiveRelayer, bytes message)
func (_SpokePool *SpokePoolFilterer) WatchFundsDeposited(opts *bind.WatchOpts, sink chan<- *SpokePoolFundsDeposited, destinationChainId []*big.Int, depositId []*big.Int, depositor [][32]byte) (event.Subscription, error) {

	var destinationChainIdRule []interface{}
	for _, destinationChainIdItem := range destinationChainId {
		destinationChainIdRule = append(destinationChainIdRule, destinationChainIdItem)
	}
	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}

	var depositorRule []interface{}
	for _, depositorItem := range depositor {
		depositorRule = append(depositorRule, depositorItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "FundsDeposited", destinationChainIdRule, depositIdRule, depositorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolFundsDeposited)
				if err := _SpokePool.contract.UnpackLog(event, "FundsDeposited", log); err != nil {
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

// ParseFundsDeposited is a log parse operation binding the contract event 0x32ed1a409ef04c7b0227189c3a103dc5ac10e775a15b785dcc510201f7c25ad3.
//
// Solidity: event FundsDeposited(bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 indexed destinationChainId, uint256 indexed depositId, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityDeadline, bytes32 indexed depositor, bytes32 recipient, bytes32 exclusiveRelayer, bytes message)
func (_SpokePool *SpokePoolFilterer) ParseFundsDeposited(log types.Log) (*SpokePoolFundsDeposited, error) {
	event := new(SpokePoolFundsDeposited)
	if err := _SpokePool.contract.UnpackLog(event, "FundsDeposited", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolInitializedIterator is returned from FilterInitialized and is used to iterate over the raw logs and unpacked data for Initialized events raised by the SpokePool contract.
type SpokePoolInitializedIterator struct {
	Event *SpokePoolInitialized // Event containing the contract specifics and raw log

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
func (it *SpokePoolInitializedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolInitialized)
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
		it.Event = new(SpokePoolInitialized)
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
func (it *SpokePoolInitializedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolInitializedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolInitialized represents a Initialized event raised by the SpokePool contract.
type SpokePoolInitialized struct {
	Version uint8
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterInitialized is a free log retrieval operation binding the contract event 0x7f26b83ff96e1f2b6a682f133852f6798a09c465da95921460cefb3847402498.
//
// Solidity: event Initialized(uint8 version)
func (_SpokePool *SpokePoolFilterer) FilterInitialized(opts *bind.FilterOpts) (*SpokePoolInitializedIterator, error) {

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return &SpokePoolInitializedIterator{contract: _SpokePool.contract, event: "Initialized", logs: logs, sub: sub}, nil
}

// WatchInitialized is a free log subscription operation binding the contract event 0x7f26b83ff96e1f2b6a682f133852f6798a09c465da95921460cefb3847402498.
//
// Solidity: event Initialized(uint8 version)
func (_SpokePool *SpokePoolFilterer) WatchInitialized(opts *bind.WatchOpts, sink chan<- *SpokePoolInitialized) (event.Subscription, error) {

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolInitialized)
				if err := _SpokePool.contract.UnpackLog(event, "Initialized", log); err != nil {
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

// ParseInitialized is a log parse operation binding the contract event 0x7f26b83ff96e1f2b6a682f133852f6798a09c465da95921460cefb3847402498.
//
// Solidity: event Initialized(uint8 version)
func (_SpokePool *SpokePoolFilterer) ParseInitialized(log types.Log) (*SpokePoolInitialized, error) {
	event := new(SpokePoolInitialized)
	if err := _SpokePool.contract.UnpackLog(event, "Initialized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolOwnershipTransferredIterator is returned from FilterOwnershipTransferred and is used to iterate over the raw logs and unpacked data for OwnershipTransferred events raised by the SpokePool contract.
type SpokePoolOwnershipTransferredIterator struct {
	Event *SpokePoolOwnershipTransferred // Event containing the contract specifics and raw log

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
func (it *SpokePoolOwnershipTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolOwnershipTransferred)
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
		it.Event = new(SpokePoolOwnershipTransferred)
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
func (it *SpokePoolOwnershipTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolOwnershipTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolOwnershipTransferred represents a OwnershipTransferred event raised by the SpokePool contract.
type SpokePoolOwnershipTransferred struct {
	PreviousOwner common.Address
	NewOwner      common.Address
	Raw           types.Log // Blockchain specific contextual infos
}

// FilterOwnershipTransferred is a free log retrieval operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_SpokePool *SpokePoolFilterer) FilterOwnershipTransferred(opts *bind.FilterOpts, previousOwner []common.Address, newOwner []common.Address) (*SpokePoolOwnershipTransferredIterator, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolOwnershipTransferredIterator{contract: _SpokePool.contract, event: "OwnershipTransferred", logs: logs, sub: sub}, nil
}

// WatchOwnershipTransferred is a free log subscription operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_SpokePool *SpokePoolFilterer) WatchOwnershipTransferred(opts *bind.WatchOpts, sink chan<- *SpokePoolOwnershipTransferred, previousOwner []common.Address, newOwner []common.Address) (event.Subscription, error) {

	var previousOwnerRule []interface{}
	for _, previousOwnerItem := range previousOwner {
		previousOwnerRule = append(previousOwnerRule, previousOwnerItem)
	}
	var newOwnerRule []interface{}
	for _, newOwnerItem := range newOwner {
		newOwnerRule = append(newOwnerRule, newOwnerItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "OwnershipTransferred", previousOwnerRule, newOwnerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolOwnershipTransferred)
				if err := _SpokePool.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
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

// ParseOwnershipTransferred is a log parse operation binding the contract event 0x8be0079c531659141344cd1fd0a4f28419497f9722a3daafe3b4186f6b6457e0.
//
// Solidity: event OwnershipTransferred(address indexed previousOwner, address indexed newOwner)
func (_SpokePool *SpokePoolFilterer) ParseOwnershipTransferred(log types.Log) (*SpokePoolOwnershipTransferred, error) {
	event := new(SpokePoolOwnershipTransferred)
	if err := _SpokePool.contract.UnpackLog(event, "OwnershipTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolPausedDepositsIterator is returned from FilterPausedDeposits and is used to iterate over the raw logs and unpacked data for PausedDeposits events raised by the SpokePool contract.
type SpokePoolPausedDepositsIterator struct {
	Event *SpokePoolPausedDeposits // Event containing the contract specifics and raw log

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
func (it *SpokePoolPausedDepositsIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolPausedDeposits)
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
		it.Event = new(SpokePoolPausedDeposits)
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
func (it *SpokePoolPausedDepositsIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolPausedDepositsIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolPausedDeposits represents a PausedDeposits event raised by the SpokePool contract.
type SpokePoolPausedDeposits struct {
	IsPaused bool
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterPausedDeposits is a free log retrieval operation binding the contract event 0xe88463c2f254e2b070013a2dc7ee1e099f9bc00534cbdf03af551dc26ae49219.
//
// Solidity: event PausedDeposits(bool isPaused)
func (_SpokePool *SpokePoolFilterer) FilterPausedDeposits(opts *bind.FilterOpts) (*SpokePoolPausedDepositsIterator, error) {

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "PausedDeposits")
	if err != nil {
		return nil, err
	}
	return &SpokePoolPausedDepositsIterator{contract: _SpokePool.contract, event: "PausedDeposits", logs: logs, sub: sub}, nil
}

// WatchPausedDeposits is a free log subscription operation binding the contract event 0xe88463c2f254e2b070013a2dc7ee1e099f9bc00534cbdf03af551dc26ae49219.
//
// Solidity: event PausedDeposits(bool isPaused)
func (_SpokePool *SpokePoolFilterer) WatchPausedDeposits(opts *bind.WatchOpts, sink chan<- *SpokePoolPausedDeposits) (event.Subscription, error) {

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "PausedDeposits")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolPausedDeposits)
				if err := _SpokePool.contract.UnpackLog(event, "PausedDeposits", log); err != nil {
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

// ParsePausedDeposits is a log parse operation binding the contract event 0xe88463c2f254e2b070013a2dc7ee1e099f9bc00534cbdf03af551dc26ae49219.
//
// Solidity: event PausedDeposits(bool isPaused)
func (_SpokePool *SpokePoolFilterer) ParsePausedDeposits(log types.Log) (*SpokePoolPausedDeposits, error) {
	event := new(SpokePoolPausedDeposits)
	if err := _SpokePool.contract.UnpackLog(event, "PausedDeposits", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolPausedFillsIterator is returned from FilterPausedFills and is used to iterate over the raw logs and unpacked data for PausedFills events raised by the SpokePool contract.
type SpokePoolPausedFillsIterator struct {
	Event *SpokePoolPausedFills // Event containing the contract specifics and raw log

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
func (it *SpokePoolPausedFillsIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolPausedFills)
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
		it.Event = new(SpokePoolPausedFills)
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
func (it *SpokePoolPausedFillsIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolPausedFillsIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolPausedFills represents a PausedFills event raised by the SpokePool contract.
type SpokePoolPausedFills struct {
	IsPaused bool
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterPausedFills is a free log retrieval operation binding the contract event 0x2d5b62420992e5a4afce0e77742636ca2608ef58289fd2e1baa5161ef6e7e41e.
//
// Solidity: event PausedFills(bool isPaused)
func (_SpokePool *SpokePoolFilterer) FilterPausedFills(opts *bind.FilterOpts) (*SpokePoolPausedFillsIterator, error) {

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "PausedFills")
	if err != nil {
		return nil, err
	}
	return &SpokePoolPausedFillsIterator{contract: _SpokePool.contract, event: "PausedFills", logs: logs, sub: sub}, nil
}

// WatchPausedFills is a free log subscription operation binding the contract event 0x2d5b62420992e5a4afce0e77742636ca2608ef58289fd2e1baa5161ef6e7e41e.
//
// Solidity: event PausedFills(bool isPaused)
func (_SpokePool *SpokePoolFilterer) WatchPausedFills(opts *bind.WatchOpts, sink chan<- *SpokePoolPausedFills) (event.Subscription, error) {

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "PausedFills")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolPausedFills)
				if err := _SpokePool.contract.UnpackLog(event, "PausedFills", log); err != nil {
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

// ParsePausedFills is a log parse operation binding the contract event 0x2d5b62420992e5a4afce0e77742636ca2608ef58289fd2e1baa5161ef6e7e41e.
//
// Solidity: event PausedFills(bool isPaused)
func (_SpokePool *SpokePoolFilterer) ParsePausedFills(log types.Log) (*SpokePoolPausedFills, error) {
	event := new(SpokePoolPausedFills)
	if err := _SpokePool.contract.UnpackLog(event, "PausedFills", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolRelayedRootBundleIterator is returned from FilterRelayedRootBundle and is used to iterate over the raw logs and unpacked data for RelayedRootBundle events raised by the SpokePool contract.
type SpokePoolRelayedRootBundleIterator struct {
	Event *SpokePoolRelayedRootBundle // Event containing the contract specifics and raw log

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
func (it *SpokePoolRelayedRootBundleIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolRelayedRootBundle)
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
		it.Event = new(SpokePoolRelayedRootBundle)
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
func (it *SpokePoolRelayedRootBundleIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolRelayedRootBundleIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolRelayedRootBundle represents a RelayedRootBundle event raised by the SpokePool contract.
type SpokePoolRelayedRootBundle struct {
	RootBundleId      uint32
	RelayerRefundRoot [32]byte
	SlowRelayRoot     [32]byte
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterRelayedRootBundle is a free log retrieval operation binding the contract event 0xc86ba04c55bc5eb2f2876b91c438849a296dbec7b08751c3074d92e04f0a77af.
//
// Solidity: event RelayedRootBundle(uint32 indexed rootBundleId, bytes32 indexed relayerRefundRoot, bytes32 indexed slowRelayRoot)
func (_SpokePool *SpokePoolFilterer) FilterRelayedRootBundle(opts *bind.FilterOpts, rootBundleId []uint32, relayerRefundRoot [][32]byte, slowRelayRoot [][32]byte) (*SpokePoolRelayedRootBundleIterator, error) {

	var rootBundleIdRule []interface{}
	for _, rootBundleIdItem := range rootBundleId {
		rootBundleIdRule = append(rootBundleIdRule, rootBundleIdItem)
	}
	var relayerRefundRootRule []interface{}
	for _, relayerRefundRootItem := range relayerRefundRoot {
		relayerRefundRootRule = append(relayerRefundRootRule, relayerRefundRootItem)
	}
	var slowRelayRootRule []interface{}
	for _, slowRelayRootItem := range slowRelayRoot {
		slowRelayRootRule = append(slowRelayRootRule, slowRelayRootItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "RelayedRootBundle", rootBundleIdRule, relayerRefundRootRule, slowRelayRootRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolRelayedRootBundleIterator{contract: _SpokePool.contract, event: "RelayedRootBundle", logs: logs, sub: sub}, nil
}

// WatchRelayedRootBundle is a free log subscription operation binding the contract event 0xc86ba04c55bc5eb2f2876b91c438849a296dbec7b08751c3074d92e04f0a77af.
//
// Solidity: event RelayedRootBundle(uint32 indexed rootBundleId, bytes32 indexed relayerRefundRoot, bytes32 indexed slowRelayRoot)
func (_SpokePool *SpokePoolFilterer) WatchRelayedRootBundle(opts *bind.WatchOpts, sink chan<- *SpokePoolRelayedRootBundle, rootBundleId []uint32, relayerRefundRoot [][32]byte, slowRelayRoot [][32]byte) (event.Subscription, error) {

	var rootBundleIdRule []interface{}
	for _, rootBundleIdItem := range rootBundleId {
		rootBundleIdRule = append(rootBundleIdRule, rootBundleIdItem)
	}
	var relayerRefundRootRule []interface{}
	for _, relayerRefundRootItem := range relayerRefundRoot {
		relayerRefundRootRule = append(relayerRefundRootRule, relayerRefundRootItem)
	}
	var slowRelayRootRule []interface{}
	for _, slowRelayRootItem := range slowRelayRoot {
		slowRelayRootRule = append(slowRelayRootRule, slowRelayRootItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "RelayedRootBundle", rootBundleIdRule, relayerRefundRootRule, slowRelayRootRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolRelayedRootBundle)
				if err := _SpokePool.contract.UnpackLog(event, "RelayedRootBundle", log); err != nil {
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

// ParseRelayedRootBundle is a log parse operation binding the contract event 0xc86ba04c55bc5eb2f2876b91c438849a296dbec7b08751c3074d92e04f0a77af.
//
// Solidity: event RelayedRootBundle(uint32 indexed rootBundleId, bytes32 indexed relayerRefundRoot, bytes32 indexed slowRelayRoot)
func (_SpokePool *SpokePoolFilterer) ParseRelayedRootBundle(log types.Log) (*SpokePoolRelayedRootBundle, error) {
	event := new(SpokePoolRelayedRootBundle)
	if err := _SpokePool.contract.UnpackLog(event, "RelayedRootBundle", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolRequestedSlowFillIterator is returned from FilterRequestedSlowFill and is used to iterate over the raw logs and unpacked data for RequestedSlowFill events raised by the SpokePool contract.
type SpokePoolRequestedSlowFillIterator struct {
	Event *SpokePoolRequestedSlowFill // Event containing the contract specifics and raw log

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
func (it *SpokePoolRequestedSlowFillIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolRequestedSlowFill)
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
		it.Event = new(SpokePoolRequestedSlowFill)
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
func (it *SpokePoolRequestedSlowFillIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolRequestedSlowFillIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolRequestedSlowFill represents a RequestedSlowFill event raised by the SpokePool contract.
type SpokePoolRequestedSlowFill struct {
	InputToken          [32]byte
	OutputToken         [32]byte
	InputAmount         *big.Int
	OutputAmount        *big.Int
	OriginChainId       *big.Int
	DepositId           *big.Int
	FillDeadline        uint32
	ExclusivityDeadline uint32
	ExclusiveRelayer    [32]byte
	Depositor           [32]byte
	Recipient           [32]byte
	MessageHash         [32]byte
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterRequestedSlowFill is a free log retrieval operation binding the contract event 0x3cee3e290f36226751cd0b3321b213890fe9c768e922f267fa6111836ce05c32.
//
// Solidity: event RequestedSlowFill(bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 indexed originChainId, uint256 indexed depositId, uint32 fillDeadline, uint32 exclusivityDeadline, bytes32 exclusiveRelayer, bytes32 depositor, bytes32 recipient, bytes32 messageHash)
func (_SpokePool *SpokePoolFilterer) FilterRequestedSlowFill(opts *bind.FilterOpts, originChainId []*big.Int, depositId []*big.Int) (*SpokePoolRequestedSlowFillIterator, error) {

	var originChainIdRule []interface{}
	for _, originChainIdItem := range originChainId {
		originChainIdRule = append(originChainIdRule, originChainIdItem)
	}
	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "RequestedSlowFill", originChainIdRule, depositIdRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolRequestedSlowFillIterator{contract: _SpokePool.contract, event: "RequestedSlowFill", logs: logs, sub: sub}, nil
}

// WatchRequestedSlowFill is a free log subscription operation binding the contract event 0x3cee3e290f36226751cd0b3321b213890fe9c768e922f267fa6111836ce05c32.
//
// Solidity: event RequestedSlowFill(bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 indexed originChainId, uint256 indexed depositId, uint32 fillDeadline, uint32 exclusivityDeadline, bytes32 exclusiveRelayer, bytes32 depositor, bytes32 recipient, bytes32 messageHash)
func (_SpokePool *SpokePoolFilterer) WatchRequestedSlowFill(opts *bind.WatchOpts, sink chan<- *SpokePoolRequestedSlowFill, originChainId []*big.Int, depositId []*big.Int) (event.Subscription, error) {

	var originChainIdRule []interface{}
	for _, originChainIdItem := range originChainId {
		originChainIdRule = append(originChainIdRule, originChainIdItem)
	}
	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "RequestedSlowFill", originChainIdRule, depositIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolRequestedSlowFill)
				if err := _SpokePool.contract.UnpackLog(event, "RequestedSlowFill", log); err != nil {
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

// ParseRequestedSlowFill is a log parse operation binding the contract event 0x3cee3e290f36226751cd0b3321b213890fe9c768e922f267fa6111836ce05c32.
//
// Solidity: event RequestedSlowFill(bytes32 inputToken, bytes32 outputToken, uint256 inputAmount, uint256 outputAmount, uint256 indexed originChainId, uint256 indexed depositId, uint32 fillDeadline, uint32 exclusivityDeadline, bytes32 exclusiveRelayer, bytes32 depositor, bytes32 recipient, bytes32 messageHash)
func (_SpokePool *SpokePoolFilterer) ParseRequestedSlowFill(log types.Log) (*SpokePoolRequestedSlowFill, error) {
	event := new(SpokePoolRequestedSlowFill)
	if err := _SpokePool.contract.UnpackLog(event, "RequestedSlowFill", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolRequestedSpeedUpDepositIterator is returned from FilterRequestedSpeedUpDeposit and is used to iterate over the raw logs and unpacked data for RequestedSpeedUpDeposit events raised by the SpokePool contract.
type SpokePoolRequestedSpeedUpDepositIterator struct {
	Event *SpokePoolRequestedSpeedUpDeposit // Event containing the contract specifics and raw log

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
func (it *SpokePoolRequestedSpeedUpDepositIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolRequestedSpeedUpDeposit)
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
		it.Event = new(SpokePoolRequestedSpeedUpDeposit)
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
func (it *SpokePoolRequestedSpeedUpDepositIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolRequestedSpeedUpDepositIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolRequestedSpeedUpDeposit represents a RequestedSpeedUpDeposit event raised by the SpokePool contract.
type SpokePoolRequestedSpeedUpDeposit struct {
	UpdatedOutputAmount *big.Int
	DepositId           *big.Int
	Depositor           [32]byte
	UpdatedRecipient    [32]byte
	UpdatedMessage      []byte
	DepositorSignature  []byte
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterRequestedSpeedUpDeposit is a free log retrieval operation binding the contract event 0x45e04bc8f121ba11466985789ca2822a91109f31bb8ac85504a37b7eaf873c26.
//
// Solidity: event RequestedSpeedUpDeposit(uint256 updatedOutputAmount, uint256 indexed depositId, bytes32 indexed depositor, bytes32 updatedRecipient, bytes updatedMessage, bytes depositorSignature)
func (_SpokePool *SpokePoolFilterer) FilterRequestedSpeedUpDeposit(opts *bind.FilterOpts, depositId []*big.Int, depositor [][32]byte) (*SpokePoolRequestedSpeedUpDepositIterator, error) {

	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}
	var depositorRule []interface{}
	for _, depositorItem := range depositor {
		depositorRule = append(depositorRule, depositorItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "RequestedSpeedUpDeposit", depositIdRule, depositorRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolRequestedSpeedUpDepositIterator{contract: _SpokePool.contract, event: "RequestedSpeedUpDeposit", logs: logs, sub: sub}, nil
}

// WatchRequestedSpeedUpDeposit is a free log subscription operation binding the contract event 0x45e04bc8f121ba11466985789ca2822a91109f31bb8ac85504a37b7eaf873c26.
//
// Solidity: event RequestedSpeedUpDeposit(uint256 updatedOutputAmount, uint256 indexed depositId, bytes32 indexed depositor, bytes32 updatedRecipient, bytes updatedMessage, bytes depositorSignature)
func (_SpokePool *SpokePoolFilterer) WatchRequestedSpeedUpDeposit(opts *bind.WatchOpts, sink chan<- *SpokePoolRequestedSpeedUpDeposit, depositId []*big.Int, depositor [][32]byte) (event.Subscription, error) {

	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}
	var depositorRule []interface{}
	for _, depositorItem := range depositor {
		depositorRule = append(depositorRule, depositorItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "RequestedSpeedUpDeposit", depositIdRule, depositorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolRequestedSpeedUpDeposit)
				if err := _SpokePool.contract.UnpackLog(event, "RequestedSpeedUpDeposit", log); err != nil {
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

// ParseRequestedSpeedUpDeposit is a log parse operation binding the contract event 0x45e04bc8f121ba11466985789ca2822a91109f31bb8ac85504a37b7eaf873c26.
//
// Solidity: event RequestedSpeedUpDeposit(uint256 updatedOutputAmount, uint256 indexed depositId, bytes32 indexed depositor, bytes32 updatedRecipient, bytes updatedMessage, bytes depositorSignature)
func (_SpokePool *SpokePoolFilterer) ParseRequestedSpeedUpDeposit(log types.Log) (*SpokePoolRequestedSpeedUpDeposit, error) {
	event := new(SpokePoolRequestedSpeedUpDeposit)
	if err := _SpokePool.contract.UnpackLog(event, "RequestedSpeedUpDeposit", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolRequestedSpeedUpV3DepositIterator is returned from FilterRequestedSpeedUpV3Deposit and is used to iterate over the raw logs and unpacked data for RequestedSpeedUpV3Deposit events raised by the SpokePool contract.
type SpokePoolRequestedSpeedUpV3DepositIterator struct {
	Event *SpokePoolRequestedSpeedUpV3Deposit // Event containing the contract specifics and raw log

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
func (it *SpokePoolRequestedSpeedUpV3DepositIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolRequestedSpeedUpV3Deposit)
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
		it.Event = new(SpokePoolRequestedSpeedUpV3Deposit)
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
func (it *SpokePoolRequestedSpeedUpV3DepositIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolRequestedSpeedUpV3DepositIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolRequestedSpeedUpV3Deposit represents a RequestedSpeedUpV3Deposit event raised by the SpokePool contract.
type SpokePoolRequestedSpeedUpV3Deposit struct {
	UpdatedOutputAmount *big.Int
	DepositId           uint32
	Depositor           common.Address
	UpdatedRecipient    common.Address
	UpdatedMessage      []byte
	DepositorSignature  []byte
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterRequestedSpeedUpV3Deposit is a free log retrieval operation binding the contract event 0xb0a29aed3d389a1041194255878b423f7780be3ed2324d4693508c6ff189845e.
//
// Solidity: event RequestedSpeedUpV3Deposit(uint256 updatedOutputAmount, uint32 indexed depositId, address indexed depositor, address updatedRecipient, bytes updatedMessage, bytes depositorSignature)
func (_SpokePool *SpokePoolFilterer) FilterRequestedSpeedUpV3Deposit(opts *bind.FilterOpts, depositId []uint32, depositor []common.Address) (*SpokePoolRequestedSpeedUpV3DepositIterator, error) {

	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}
	var depositorRule []interface{}
	for _, depositorItem := range depositor {
		depositorRule = append(depositorRule, depositorItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "RequestedSpeedUpV3Deposit", depositIdRule, depositorRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolRequestedSpeedUpV3DepositIterator{contract: _SpokePool.contract, event: "RequestedSpeedUpV3Deposit", logs: logs, sub: sub}, nil
}

// WatchRequestedSpeedUpV3Deposit is a free log subscription operation binding the contract event 0xb0a29aed3d389a1041194255878b423f7780be3ed2324d4693508c6ff189845e.
//
// Solidity: event RequestedSpeedUpV3Deposit(uint256 updatedOutputAmount, uint32 indexed depositId, address indexed depositor, address updatedRecipient, bytes updatedMessage, bytes depositorSignature)
func (_SpokePool *SpokePoolFilterer) WatchRequestedSpeedUpV3Deposit(opts *bind.WatchOpts, sink chan<- *SpokePoolRequestedSpeedUpV3Deposit, depositId []uint32, depositor []common.Address) (event.Subscription, error) {

	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}
	var depositorRule []interface{}
	for _, depositorItem := range depositor {
		depositorRule = append(depositorRule, depositorItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "RequestedSpeedUpV3Deposit", depositIdRule, depositorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolRequestedSpeedUpV3Deposit)
				if err := _SpokePool.contract.UnpackLog(event, "RequestedSpeedUpV3Deposit", log); err != nil {
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

// ParseRequestedSpeedUpV3Deposit is a log parse operation binding the contract event 0xb0a29aed3d389a1041194255878b423f7780be3ed2324d4693508c6ff189845e.
//
// Solidity: event RequestedSpeedUpV3Deposit(uint256 updatedOutputAmount, uint32 indexed depositId, address indexed depositor, address updatedRecipient, bytes updatedMessage, bytes depositorSignature)
func (_SpokePool *SpokePoolFilterer) ParseRequestedSpeedUpV3Deposit(log types.Log) (*SpokePoolRequestedSpeedUpV3Deposit, error) {
	event := new(SpokePoolRequestedSpeedUpV3Deposit)
	if err := _SpokePool.contract.UnpackLog(event, "RequestedSpeedUpV3Deposit", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolRequestedV3SlowFillIterator is returned from FilterRequestedV3SlowFill and is used to iterate over the raw logs and unpacked data for RequestedV3SlowFill events raised by the SpokePool contract.
type SpokePoolRequestedV3SlowFillIterator struct {
	Event *SpokePoolRequestedV3SlowFill // Event containing the contract specifics and raw log

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
func (it *SpokePoolRequestedV3SlowFillIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolRequestedV3SlowFill)
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
		it.Event = new(SpokePoolRequestedV3SlowFill)
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
func (it *SpokePoolRequestedV3SlowFillIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolRequestedV3SlowFillIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolRequestedV3SlowFill represents a RequestedV3SlowFill event raised by the SpokePool contract.
type SpokePoolRequestedV3SlowFill struct {
	InputToken          common.Address
	OutputToken         common.Address
	InputAmount         *big.Int
	OutputAmount        *big.Int
	OriginChainId       *big.Int
	DepositId           uint32
	FillDeadline        uint32
	ExclusivityDeadline uint32
	ExclusiveRelayer    common.Address
	Depositor           common.Address
	Recipient           common.Address
	Message             []byte
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterRequestedV3SlowFill is a free log retrieval operation binding the contract event 0x923794976d026d6b119735adc163cb71decfc903e17c3dc226c00789593c04e1.
//
// Solidity: event RequestedV3SlowFill(address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 indexed originChainId, uint32 indexed depositId, uint32 fillDeadline, uint32 exclusivityDeadline, address exclusiveRelayer, address depositor, address recipient, bytes message)
func (_SpokePool *SpokePoolFilterer) FilterRequestedV3SlowFill(opts *bind.FilterOpts, originChainId []*big.Int, depositId []uint32) (*SpokePoolRequestedV3SlowFillIterator, error) {

	var originChainIdRule []interface{}
	for _, originChainIdItem := range originChainId {
		originChainIdRule = append(originChainIdRule, originChainIdItem)
	}
	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "RequestedV3SlowFill", originChainIdRule, depositIdRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolRequestedV3SlowFillIterator{contract: _SpokePool.contract, event: "RequestedV3SlowFill", logs: logs, sub: sub}, nil
}

// WatchRequestedV3SlowFill is a free log subscription operation binding the contract event 0x923794976d026d6b119735adc163cb71decfc903e17c3dc226c00789593c04e1.
//
// Solidity: event RequestedV3SlowFill(address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 indexed originChainId, uint32 indexed depositId, uint32 fillDeadline, uint32 exclusivityDeadline, address exclusiveRelayer, address depositor, address recipient, bytes message)
func (_SpokePool *SpokePoolFilterer) WatchRequestedV3SlowFill(opts *bind.WatchOpts, sink chan<- *SpokePoolRequestedV3SlowFill, originChainId []*big.Int, depositId []uint32) (event.Subscription, error) {

	var originChainIdRule []interface{}
	for _, originChainIdItem := range originChainId {
		originChainIdRule = append(originChainIdRule, originChainIdItem)
	}
	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "RequestedV3SlowFill", originChainIdRule, depositIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolRequestedV3SlowFill)
				if err := _SpokePool.contract.UnpackLog(event, "RequestedV3SlowFill", log); err != nil {
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

// ParseRequestedV3SlowFill is a log parse operation binding the contract event 0x923794976d026d6b119735adc163cb71decfc903e17c3dc226c00789593c04e1.
//
// Solidity: event RequestedV3SlowFill(address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 indexed originChainId, uint32 indexed depositId, uint32 fillDeadline, uint32 exclusivityDeadline, address exclusiveRelayer, address depositor, address recipient, bytes message)
func (_SpokePool *SpokePoolFilterer) ParseRequestedV3SlowFill(log types.Log) (*SpokePoolRequestedV3SlowFill, error) {
	event := new(SpokePoolRequestedV3SlowFill)
	if err := _SpokePool.contract.UnpackLog(event, "RequestedV3SlowFill", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolSetWithdrawalRecipientIterator is returned from FilterSetWithdrawalRecipient and is used to iterate over the raw logs and unpacked data for SetWithdrawalRecipient events raised by the SpokePool contract.
type SpokePoolSetWithdrawalRecipientIterator struct {
	Event *SpokePoolSetWithdrawalRecipient // Event containing the contract specifics and raw log

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
func (it *SpokePoolSetWithdrawalRecipientIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolSetWithdrawalRecipient)
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
		it.Event = new(SpokePoolSetWithdrawalRecipient)
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
func (it *SpokePoolSetWithdrawalRecipientIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolSetWithdrawalRecipientIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolSetWithdrawalRecipient represents a SetWithdrawalRecipient event raised by the SpokePool contract.
type SpokePoolSetWithdrawalRecipient struct {
	NewWithdrawalRecipient common.Address
	Raw                    types.Log // Blockchain specific contextual infos
}

// FilterSetWithdrawalRecipient is a free log retrieval operation binding the contract event 0xa73e8909f8616742d7fe701153d82666f7b7cd480552e23ebb05d358c22fd04e.
//
// Solidity: event SetWithdrawalRecipient(address indexed newWithdrawalRecipient)
func (_SpokePool *SpokePoolFilterer) FilterSetWithdrawalRecipient(opts *bind.FilterOpts, newWithdrawalRecipient []common.Address) (*SpokePoolSetWithdrawalRecipientIterator, error) {

	var newWithdrawalRecipientRule []interface{}
	for _, newWithdrawalRecipientItem := range newWithdrawalRecipient {
		newWithdrawalRecipientRule = append(newWithdrawalRecipientRule, newWithdrawalRecipientItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "SetWithdrawalRecipient", newWithdrawalRecipientRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolSetWithdrawalRecipientIterator{contract: _SpokePool.contract, event: "SetWithdrawalRecipient", logs: logs, sub: sub}, nil
}

// WatchSetWithdrawalRecipient is a free log subscription operation binding the contract event 0xa73e8909f8616742d7fe701153d82666f7b7cd480552e23ebb05d358c22fd04e.
//
// Solidity: event SetWithdrawalRecipient(address indexed newWithdrawalRecipient)
func (_SpokePool *SpokePoolFilterer) WatchSetWithdrawalRecipient(opts *bind.WatchOpts, sink chan<- *SpokePoolSetWithdrawalRecipient, newWithdrawalRecipient []common.Address) (event.Subscription, error) {

	var newWithdrawalRecipientRule []interface{}
	for _, newWithdrawalRecipientItem := range newWithdrawalRecipient {
		newWithdrawalRecipientRule = append(newWithdrawalRecipientRule, newWithdrawalRecipientItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "SetWithdrawalRecipient", newWithdrawalRecipientRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolSetWithdrawalRecipient)
				if err := _SpokePool.contract.UnpackLog(event, "SetWithdrawalRecipient", log); err != nil {
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

// ParseSetWithdrawalRecipient is a log parse operation binding the contract event 0xa73e8909f8616742d7fe701153d82666f7b7cd480552e23ebb05d358c22fd04e.
//
// Solidity: event SetWithdrawalRecipient(address indexed newWithdrawalRecipient)
func (_SpokePool *SpokePoolFilterer) ParseSetWithdrawalRecipient(log types.Log) (*SpokePoolSetWithdrawalRecipient, error) {
	event := new(SpokePoolSetWithdrawalRecipient)
	if err := _SpokePool.contract.UnpackLog(event, "SetWithdrawalRecipient", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolSetXDomainAdminIterator is returned from FilterSetXDomainAdmin and is used to iterate over the raw logs and unpacked data for SetXDomainAdmin events raised by the SpokePool contract.
type SpokePoolSetXDomainAdminIterator struct {
	Event *SpokePoolSetXDomainAdmin // Event containing the contract specifics and raw log

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
func (it *SpokePoolSetXDomainAdminIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolSetXDomainAdmin)
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
		it.Event = new(SpokePoolSetXDomainAdmin)
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
func (it *SpokePoolSetXDomainAdminIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolSetXDomainAdminIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolSetXDomainAdmin represents a SetXDomainAdmin event raised by the SpokePool contract.
type SpokePoolSetXDomainAdmin struct {
	NewAdmin common.Address
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterSetXDomainAdmin is a free log retrieval operation binding the contract event 0xa9e8c42c9e7fca7f62755189a16b2f5314d43d8fb24e91ba54e6d65f9314e849.
//
// Solidity: event SetXDomainAdmin(address indexed newAdmin)
func (_SpokePool *SpokePoolFilterer) FilterSetXDomainAdmin(opts *bind.FilterOpts, newAdmin []common.Address) (*SpokePoolSetXDomainAdminIterator, error) {

	var newAdminRule []interface{}
	for _, newAdminItem := range newAdmin {
		newAdminRule = append(newAdminRule, newAdminItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "SetXDomainAdmin", newAdminRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolSetXDomainAdminIterator{contract: _SpokePool.contract, event: "SetXDomainAdmin", logs: logs, sub: sub}, nil
}

// WatchSetXDomainAdmin is a free log subscription operation binding the contract event 0xa9e8c42c9e7fca7f62755189a16b2f5314d43d8fb24e91ba54e6d65f9314e849.
//
// Solidity: event SetXDomainAdmin(address indexed newAdmin)
func (_SpokePool *SpokePoolFilterer) WatchSetXDomainAdmin(opts *bind.WatchOpts, sink chan<- *SpokePoolSetXDomainAdmin, newAdmin []common.Address) (event.Subscription, error) {

	var newAdminRule []interface{}
	for _, newAdminItem := range newAdmin {
		newAdminRule = append(newAdminRule, newAdminItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "SetXDomainAdmin", newAdminRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolSetXDomainAdmin)
				if err := _SpokePool.contract.UnpackLog(event, "SetXDomainAdmin", log); err != nil {
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

// ParseSetXDomainAdmin is a log parse operation binding the contract event 0xa9e8c42c9e7fca7f62755189a16b2f5314d43d8fb24e91ba54e6d65f9314e849.
//
// Solidity: event SetXDomainAdmin(address indexed newAdmin)
func (_SpokePool *SpokePoolFilterer) ParseSetXDomainAdmin(log types.Log) (*SpokePoolSetXDomainAdmin, error) {
	event := new(SpokePoolSetXDomainAdmin)
	if err := _SpokePool.contract.UnpackLog(event, "SetXDomainAdmin", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolTokensBridgedIterator is returned from FilterTokensBridged and is used to iterate over the raw logs and unpacked data for TokensBridged events raised by the SpokePool contract.
type SpokePoolTokensBridgedIterator struct {
	Event *SpokePoolTokensBridged // Event containing the contract specifics and raw log

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
func (it *SpokePoolTokensBridgedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolTokensBridged)
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
		it.Event = new(SpokePoolTokensBridged)
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
func (it *SpokePoolTokensBridgedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolTokensBridgedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolTokensBridged represents a TokensBridged event raised by the SpokePool contract.
type SpokePoolTokensBridged struct {
	AmountToReturn *big.Int
	ChainId        *big.Int
	LeafId         uint32
	L2TokenAddress [32]byte
	Caller         common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterTokensBridged is a free log retrieval operation binding the contract event 0xfa7fa7cf6d7dde5f9be65a67e6a1a747e7aa864dcd2d793353c722d80fbbb357.
//
// Solidity: event TokensBridged(uint256 amountToReturn, uint256 indexed chainId, uint32 indexed leafId, bytes32 indexed l2TokenAddress, address caller)
func (_SpokePool *SpokePoolFilterer) FilterTokensBridged(opts *bind.FilterOpts, chainId []*big.Int, leafId []uint32, l2TokenAddress [][32]byte) (*SpokePoolTokensBridgedIterator, error) {

	var chainIdRule []interface{}
	for _, chainIdItem := range chainId {
		chainIdRule = append(chainIdRule, chainIdItem)
	}
	var leafIdRule []interface{}
	for _, leafIdItem := range leafId {
		leafIdRule = append(leafIdRule, leafIdItem)
	}
	var l2TokenAddressRule []interface{}
	for _, l2TokenAddressItem := range l2TokenAddress {
		l2TokenAddressRule = append(l2TokenAddressRule, l2TokenAddressItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "TokensBridged", chainIdRule, leafIdRule, l2TokenAddressRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolTokensBridgedIterator{contract: _SpokePool.contract, event: "TokensBridged", logs: logs, sub: sub}, nil
}

// WatchTokensBridged is a free log subscription operation binding the contract event 0xfa7fa7cf6d7dde5f9be65a67e6a1a747e7aa864dcd2d793353c722d80fbbb357.
//
// Solidity: event TokensBridged(uint256 amountToReturn, uint256 indexed chainId, uint32 indexed leafId, bytes32 indexed l2TokenAddress, address caller)
func (_SpokePool *SpokePoolFilterer) WatchTokensBridged(opts *bind.WatchOpts, sink chan<- *SpokePoolTokensBridged, chainId []*big.Int, leafId []uint32, l2TokenAddress [][32]byte) (event.Subscription, error) {

	var chainIdRule []interface{}
	for _, chainIdItem := range chainId {
		chainIdRule = append(chainIdRule, chainIdItem)
	}
	var leafIdRule []interface{}
	for _, leafIdItem := range leafId {
		leafIdRule = append(leafIdRule, leafIdItem)
	}
	var l2TokenAddressRule []interface{}
	for _, l2TokenAddressItem := range l2TokenAddress {
		l2TokenAddressRule = append(l2TokenAddressRule, l2TokenAddressItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "TokensBridged", chainIdRule, leafIdRule, l2TokenAddressRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolTokensBridged)
				if err := _SpokePool.contract.UnpackLog(event, "TokensBridged", log); err != nil {
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

// ParseTokensBridged is a log parse operation binding the contract event 0xfa7fa7cf6d7dde5f9be65a67e6a1a747e7aa864dcd2d793353c722d80fbbb357.
//
// Solidity: event TokensBridged(uint256 amountToReturn, uint256 indexed chainId, uint32 indexed leafId, bytes32 indexed l2TokenAddress, address caller)
func (_SpokePool *SpokePoolFilterer) ParseTokensBridged(log types.Log) (*SpokePoolTokensBridged, error) {
	event := new(SpokePoolTokensBridged)
	if err := _SpokePool.contract.UnpackLog(event, "TokensBridged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolUpgradedIterator is returned from FilterUpgraded and is used to iterate over the raw logs and unpacked data for Upgraded events raised by the SpokePool contract.
type SpokePoolUpgradedIterator struct {
	Event *SpokePoolUpgraded // Event containing the contract specifics and raw log

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
func (it *SpokePoolUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolUpgraded)
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
		it.Event = new(SpokePoolUpgraded)
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
func (it *SpokePoolUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolUpgraded represents a Upgraded event raised by the SpokePool contract.
type SpokePoolUpgraded struct {
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterUpgraded is a free log retrieval operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_SpokePool *SpokePoolFilterer) FilterUpgraded(opts *bind.FilterOpts, implementation []common.Address) (*SpokePoolUpgradedIterator, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolUpgradedIterator{contract: _SpokePool.contract, event: "Upgraded", logs: logs, sub: sub}, nil
}

// WatchUpgraded is a free log subscription operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_SpokePool *SpokePoolFilterer) WatchUpgraded(opts *bind.WatchOpts, sink chan<- *SpokePoolUpgraded, implementation []common.Address) (event.Subscription, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolUpgraded)
				if err := _SpokePool.contract.UnpackLog(event, "Upgraded", log); err != nil {
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

// ParseUpgraded is a log parse operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_SpokePool *SpokePoolFilterer) ParseUpgraded(log types.Log) (*SpokePoolUpgraded, error) {
	event := new(SpokePoolUpgraded)
	if err := _SpokePool.contract.UnpackLog(event, "Upgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SpokePoolV3FundsDepositedIterator is returned from FilterV3FundsDeposited and is used to iterate over the raw logs and unpacked data for V3FundsDeposited events raised by the SpokePool contract.
type SpokePoolV3FundsDepositedIterator struct {
	Event *SpokePoolV3FundsDeposited // Event containing the contract specifics and raw log

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
func (it *SpokePoolV3FundsDepositedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SpokePoolV3FundsDeposited)
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
		it.Event = new(SpokePoolV3FundsDeposited)
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
func (it *SpokePoolV3FundsDepositedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SpokePoolV3FundsDepositedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SpokePoolV3FundsDeposited represents a V3FundsDeposited event raised by the SpokePool contract.
type SpokePoolV3FundsDeposited struct {
	InputToken          common.Address
	OutputToken         common.Address
	InputAmount         *big.Int
	OutputAmount        *big.Int
	DestinationChainId  *big.Int
	DepositId           uint32
	QuoteTimestamp      uint32
	FillDeadline        uint32
	ExclusivityDeadline uint32
	Depositor           common.Address
	Recipient           common.Address
	ExclusiveRelayer    common.Address
	Message             []byte
	Raw                 types.Log // Blockchain specific contextual infos
}

// FilterV3FundsDeposited is a free log retrieval operation binding the contract event 0xa123dc29aebf7d0c3322c8eeb5b999e859f39937950ed31056532713d0de396f.
//
// Solidity: event V3FundsDeposited(address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 indexed destinationChainId, uint32 indexed depositId, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityDeadline, address indexed depositor, address recipient, address exclusiveRelayer, bytes message)
func (_SpokePool *SpokePoolFilterer) FilterV3FundsDeposited(opts *bind.FilterOpts, destinationChainId []*big.Int, depositId []uint32, depositor []common.Address) (*SpokePoolV3FundsDepositedIterator, error) {

	var destinationChainIdRule []interface{}
	for _, destinationChainIdItem := range destinationChainId {
		destinationChainIdRule = append(destinationChainIdRule, destinationChainIdItem)
	}
	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}

	var depositorRule []interface{}
	for _, depositorItem := range depositor {
		depositorRule = append(depositorRule, depositorItem)
	}

	logs, sub, err := _SpokePool.contract.FilterLogs(opts, "V3FundsDeposited", destinationChainIdRule, depositIdRule, depositorRule)
	if err != nil {
		return nil, err
	}
	return &SpokePoolV3FundsDepositedIterator{contract: _SpokePool.contract, event: "V3FundsDeposited", logs: logs, sub: sub}, nil
}

// WatchV3FundsDeposited is a free log subscription operation binding the contract event 0xa123dc29aebf7d0c3322c8eeb5b999e859f39937950ed31056532713d0de396f.
//
// Solidity: event V3FundsDeposited(address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 indexed destinationChainId, uint32 indexed depositId, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityDeadline, address indexed depositor, address recipient, address exclusiveRelayer, bytes message)
func (_SpokePool *SpokePoolFilterer) WatchV3FundsDeposited(opts *bind.WatchOpts, sink chan<- *SpokePoolV3FundsDeposited, destinationChainId []*big.Int, depositId []uint32, depositor []common.Address) (event.Subscription, error) {

	var destinationChainIdRule []interface{}
	for _, destinationChainIdItem := range destinationChainId {
		destinationChainIdRule = append(destinationChainIdRule, destinationChainIdItem)
	}
	var depositIdRule []interface{}
	for _, depositIdItem := range depositId {
		depositIdRule = append(depositIdRule, depositIdItem)
	}

	var depositorRule []interface{}
	for _, depositorItem := range depositor {
		depositorRule = append(depositorRule, depositorItem)
	}

	logs, sub, err := _SpokePool.contract.WatchLogs(opts, "V3FundsDeposited", destinationChainIdRule, depositIdRule, depositorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SpokePoolV3FundsDeposited)
				if err := _SpokePool.contract.UnpackLog(event, "V3FundsDeposited", log); err != nil {
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

// ParseV3FundsDeposited is a log parse operation binding the contract event 0xa123dc29aebf7d0c3322c8eeb5b999e859f39937950ed31056532713d0de396f.
//
// Solidity: event V3FundsDeposited(address inputToken, address outputToken, uint256 inputAmount, uint256 outputAmount, uint256 indexed destinationChainId, uint32 indexed depositId, uint32 quoteTimestamp, uint32 fillDeadline, uint32 exclusivityDeadline, address indexed depositor, address recipient, address exclusiveRelayer, bytes message)
func (_SpokePool *SpokePoolFilterer) ParseV3FundsDeposited(log types.Log) (*SpokePoolV3FundsDeposited, error) {
	event := new(SpokePoolV3FundsDeposited)
	if err := _SpokePool.contract.UnpackLog(event, "V3FundsDeposited", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
