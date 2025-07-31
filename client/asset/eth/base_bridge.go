package eth

import (
	"context"
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
		ethID: baseID,
	},
	dex.Testnet: {
		ethID: baseID,
	},
}

// Same for all networks.
var l2CrossDomainMessengerAddr = common.HexToAddress("0x4200000000000000000000000000000000000007")

type baseBridgeAddresses struct {
	// rootChainManagerAddr is the address of the root chain manager contract.
	rootChainManagerAddr common.Address
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
		rootChainManagerAddr: common.HexToAddress("0x3154Cf16ccdb4C6d922629664174b904d80F2C35"),
	},
	dex.Testnet: {
		rootChainManagerAddr: common.HexToAddress("0xfd0Bf71F60660E2f608ed56e1659C450eB113120"),
	},
}

// baseBridgeEth is used to manage the bridge operations on Ethereum
// for the native ETH asset.
type baseBridgeEth struct {
	rootChainManager       *basebridge.L1StandardBridgeTransactor
	l2CrossDomainMessenger *basebridge.L2CrossDomainMessenger
	cb                     bind.ContractBackend
	log                    dex.Logger
	addr                   common.Address
	net                    dex.Network
}

var _ bridge = (*baseBridgeEth)(nil)

func newBaseBridgeEth(cb bind.ContractBackend, net dex.Network, addr common.Address, log dex.Logger) (*baseBridgeEth, error) {
	addrs, found := baseBridgeAddrs[net]
	if !found {
		return nil, fmt.Errorf("no root chain manager address found for network %s", net)
	}

	rootChainManager, err := basebridge.NewL1StandardBridgeTransactor(addrs.rootChainManagerAddr, cb)
	if err != nil {
		return nil, err
	}

	l2CrossDomainMessenger, err := basebridge.NewL2CrossDomainMessenger(l2CrossDomainMessengerAddr, cb)
	if err != nil {
		return nil, err
	}

	return &baseBridgeEth{
		rootChainManager:       rootChainManager,
		l2CrossDomainMessenger: l2CrossDomainMessenger,
		cb:                     cb,
		log:                    log,
		addr:                   addr,
		net:                    net,
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

func (b *baseBridgeEth) initiateBridge(opts *bind.TransactOpts, destAssetID uint32, amt *big.Int) (*types.Transaction, bool, error) {
	expectedDestAssetID, _ := BaseBridgeSupportedAsset(ethID, b.net)
	if expectedDestAssetID != destAssetID {
		return nil, false, fmt.Errorf("%s cannot be bridged to %s", dex.BipIDSymbol(ethID), dex.BipIDSymbol(expectedDestAssetID))
	}

	const minGasLimit = 200_000
	opts.Value = amt
	tx, err := b.rootChainManager.DepositETH(opts, minGasLimit, []byte{})
	return tx, false, err
}

func (b *baseBridgeEth) getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error) {
	return nil, fmt.Errorf("no completion transaction is required when bridging from eth -> base")
}

func (b *baseBridgeEth) completeBridge(opts *bind.TransactOpts, mintInfo []byte) (tx *types.Transaction, err error) {
	return nil, fmt.Errorf("no completion transaction is required when bridging from eth -> base")
}

func (b *baseBridgeEth) initiateBridgeGas() uint64 {
	return 700_000
}

func (b *baseBridgeEth) completeBridgeGas() uint64 {
	return 0
}
