package eth

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/erc20/basebridge"
)

var (
	baseID, _ = dex.BipSymbolID("base") // really weth.base
)

var baseBridgeSupportedAssets = map[dex.Network]map[uint32]uint32{
	dex.Mainnet: {
		ethID:  baseID,
		baseID: ethID,
	},
	dex.Testnet: {
		ethID:  baseID,
		baseID: ethID,
	},
}

// Same for all networks.
var (
	l2CrossDomainMessengerAddr     = common.HexToAddress("0x4200000000000000000000000000000000000007")
	l2StandardBridgeTransactorAddr = common.HexToAddress("0x4200000000000000000000000000000000000010")
	legacyERC20ETH                 = common.HexToAddress("0xDeadDeAddeAddEAddeadDEaDDEAdDeaDDeAD0000")
	unimplementedErr               = errors.New("not implemented")
)

type baseBridgeAddresses struct {
	l1StandardBridgeTransactorAddr common.Address
}

func BaseBridgeSupportedAsset(sourceAssetID uint32, net dex.Network) (destAssetID uint32, supported bool) {
	supportedAssets, found := baseBridgeSupportedAssets[net]
	if !found {
		return 0, false
	}

	destAssetID, supported = supportedAssets[sourceAssetID]
	return
}

var baseBridgeAddrs = map[dex.Network]*baseBridgeAddresses{
	dex.Mainnet: {
		l1StandardBridgeTransactorAddr: common.HexToAddress("0x3154Cf16ccdb4C6d922629664174b904d80F2C35"),
	},
	dex.Testnet: {
		l1StandardBridgeTransactorAddr: common.HexToAddress("0xfd0Bf71F60660E2f608ed56e1659C450eB113120"),
	},
}

// ethBridgeBase is used to manage the bridge operations on Ethereum
// for the native ETH asset.
type ethBridgeBase struct {
	l1StandardBridgeTransactor *basebridge.L1StandardBridgeTransactor
	cb                         bind.ContractBackend
	log                        dex.Logger
	addr                       common.Address
	net                        dex.Network
}

var _ bridge = (*ethBridgeBase)(nil)

func newEthBridgeBase(cb bind.ContractBackend, net dex.Network, addr common.Address, log dex.Logger) (*ethBridgeBase, error) {
	addrs, found := baseBridgeAddrs[net]
	if !found {
		return nil, fmt.Errorf("no root chain manager address found for network %s", net)
	}

	l1StandardBridgeTransactor, err := basebridge.NewL1StandardBridgeTransactor(addrs.l1StandardBridgeTransactorAddr, cb)
	if err != nil {
		return nil, err
	}

	return &ethBridgeBase{
		l1StandardBridgeTransactor: l1StandardBridgeTransactor,
		cb:                         cb,
		log:                        log,
		addr:                       addr,
		net:                        net,
	}, nil
}

func (b *ethBridgeBase) bridgeContractAddr() common.Address {
	return common.Address{}
}

func (b *ethBridgeBase) approveBridgeContract(opts *bind.TransactOpts, amt *big.Int) (*types.Transaction, error) {
	return nil, fmt.Errorf("no bridge contract")
}

func (b *ethBridgeBase) bridgeContractAllowance(ctx context.Context) (*big.Int, error) {
	return nil, fmt.Errorf("no bridge contract")
}

func (b *ethBridgeBase) requiresBridgeContractApproval() bool {
	return false
}

func (b *ethBridgeBase) requiresCompletion() bool {
	return false
}

func (b *ethBridgeBase) initiateBridge(opts *bind.TransactOpts, destAssetID uint32, amt *big.Int) (*types.Transaction, error) {
	expectedDestAssetID, _ := BaseBridgeSupportedAsset(ethID, b.net)
	if expectedDestAssetID != destAssetID {
		return nil, fmt.Errorf("%s cannot be bridged to %s", dex.BipIDSymbol(ethID), dex.BipIDSymbol(expectedDestAssetID))
	}

	const minGasLimit = 200_000
	opts.Value = amt
	return b.l1StandardBridgeTransactor.DepositETH(opts, minGasLimit, []byte{})
}

func (b *ethBridgeBase) getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error) {
	return nil, fmt.Errorf("no completion transaction is required when bridging from eth -> base")
}

func (b *ethBridgeBase) completeBridge(opts *bind.TransactOpts, mintInfo []byte) (tx *types.Transaction, err error) {
	return nil, nil
}

// TODO: Implement.
func (b *ethBridgeBase) verifyBridgeCompletion(ctx context.Context, data []byte) (bool, error) {
	return true, nil
}

func (b *ethBridgeBase) initiateBridgeGas() uint64 {
	return 700_000
}

func (b *ethBridgeBase) completeBridgeGas() uint64 {
	return 0
}

var _ bridge = (*baseBridgeEth)(nil)

// baseBridgeEth is used to manage the bridge operations on Base.
type baseBridgeEth struct {
	l2StandardBridgeTransactor *basebridge.L2StandardbridgeTransactor
	l2CrossDomainMessenger     *basebridge.L2CrossDomainMessenger
	cb                         bind.ContractBackend
	log                        dex.Logger
	addr                       common.Address
	net                        dex.Network
}

var _ bridge = (*ethBridgeBase)(nil)

func newBaseBridgeEth(cb bind.ContractBackend, net dex.Network, addr common.Address, log dex.Logger) (*baseBridgeEth, error) {
	l2StandardBridgeTransactor, err := basebridge.NewL2StandardbridgeTransactor(l2StandardBridgeTransactorAddr, cb)
	if err != nil {
		return nil, err
	}

	l2CrossDomainMessenger, err := basebridge.NewL2CrossDomainMessenger(l2CrossDomainMessengerAddr, cb)
	if err != nil {
		return nil, err
	}

	return &baseBridgeEth{
		l2StandardBridgeTransactor: l2StandardBridgeTransactor,
		l2CrossDomainMessenger:     l2CrossDomainMessenger,
		cb:                         cb,
		log:                        log,
		addr:                       addr,
		net:                        net,
	}, nil
}

func (b *baseBridgeEth) bridgeContractAddr() common.Address {
	return common.Address{}
}

func (b *baseBridgeEth) approveBridgeContract(opts *bind.TransactOpts, amt *big.Int) (*types.Transaction, error) {
	return nil, fmt.Errorf("no bridge contract")
}

func (b *baseBridgeEth) bridgeContractAllowance(ctx context.Context) (*big.Int, error) {
	return nil, fmt.Errorf("no bridge contract")
}

func (b *baseBridgeEth) requiresBridgeContractApproval() bool {
	return false
}

func (b *baseBridgeEth) requiresCompletion() bool {
	return false
}

// TODO: Implement.
func (b *baseBridgeEth) initiateBridge(opts *bind.TransactOpts, destAssetID uint32, amt *big.Int) (*types.Transaction, error) {
	return nil, unimplementedErr
	// expectedDestAssetID, _ := BaseBridgeSupportedAsset(baseID, b.net)
	// if expectedDestAssetID != destAssetID {
	// 	return nil, fmt.Errorf("%s cannot be bridged to %s", dex.BipIDSymbol(ethID), dex.BipIDSymbol(expectedDestAssetID))
	// }

	// const minGasLimit = 200_000
	// opts.Value = amt
	// return b.l2StandardBridgeTransactor.Withdraw(opts, legacyERC20ETH, amt, minGasLimit, []byte{})
}

func (b *baseBridgeEth) getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error) {
	return nil, fmt.Errorf("no completion transaction is required when bridging from eth -> base")
}

func (b *baseBridgeEth) completeBridge(opts *bind.TransactOpts, mintInfo []byte) (tx *types.Transaction, err error) {
	return nil, fmt.Errorf("no completion transaction is required when bridging from eth -> base")
}

func (b *baseBridgeEth) verifyBridgeCompletion(ctx context.Context, data []byte) (bool, error) {
	return false, unimplementedErr
}

func (b *baseBridgeEth) initiateBridgeGas() uint64 {
	return 200_000
}

func (b *baseBridgeEth) completeBridgeGas() uint64 {
	return 0
}
