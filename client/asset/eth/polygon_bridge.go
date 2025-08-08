package eth

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/dexnet"
	"decred.org/dcrdex/dex/networks/erc20"
	"decred.org/dcrdex/dex/networks/erc20/polygonbridge"
)

var (
	usdtEthID, _     = dex.BipSymbolID("usdt.eth")
	usdtPolygonID, _ = dex.BipSymbolID("usdt.polygon")
	wethPolygonID, _ = dex.BipSymbolID("weth.polygon")
	ethID, _         = dex.BipSymbolID("eth")
	polygonID, _     = dex.BipSymbolID("polygon")
	maticEthID, _    = dex.BipSymbolID("matic.eth")
)

var polygonBridgeSupportedAssets = map[dex.Network]map[uint32]uint32{
	dex.Mainnet: {
		usdtEthID:     usdtPolygonID,
		usdtPolygonID: usdtEthID,
		ethID:         wethPolygonID,
		wethPolygonID: ethID,
		maticEthID:    polygonID,
		// POL -> MATIC still needs to be implemented, but without this entry,
		// polygon will not be a bridger wallet.
		polygonID: maticEthID,
	},
	dex.Testnet: {
		ethID:         wethPolygonID,
		wethPolygonID: ethID,
		maticEthID:    polygonID,
	},
}

var stateSyncAddress = common.HexToAddress("0x0000000000000000000000000000000000001001")

const (
	transferEventSignature = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	withdrawEventSignature = "0xebff2602b3f468259e1e99f613fed6691f3a6526effe6ef3e768ba7ae7a36c4f"
	proofGeneratorURL      = "https://proof-generator.polygon.technology"
	healthCheckURL         = proofGeneratorURL + "/health-check"
)

func PolygonBridgeSupportedAsset(sourceAssetID uint32, net dex.Network) (destAssetID uint32, supported bool) {
	supportedAssets, found := polygonBridgeSupportedAssets[net]
	if !found {
		return 0, false
	}

	destAssetID, supported = supportedAssets[sourceAssetID]
	return
}

// polygonBridgePolygonERC20 performs bridge operations on Polygon for ERC20s.
type polygonBridgePolygonERC20 struct {
	cb            bind.ContractBackend
	tokenAddress  common.Address
	childERC20    *polygonbridge.ChildERC20
	stateReceiver *polygonbridge.StateReceiver
	log           dex.Logger
	assetID       uint32
	net           dex.Network
}

var _ bridge = (*polygonBridgePolygonERC20)(nil)

func newPolygonBridgePolygonErc20(cb bind.ContractBackend, assetID uint32, tokenAddress common.Address, log dex.Logger, net dex.Network) (*polygonBridgePolygonERC20, error) {
	childERC20, err := polygonbridge.NewChildERC20(tokenAddress, cb)
	if err != nil {
		return nil, err
	}

	stateReceiver, err := polygonbridge.NewStateReceiver(stateSyncAddress, cb)
	if err != nil {
		return nil, err
	}

	return &polygonBridgePolygonERC20{cb: cb, tokenAddress: tokenAddress, childERC20: childERC20, stateReceiver: stateReceiver, log: log, assetID: assetID, net: net}, nil
}

func (b *polygonBridgePolygonERC20) approveBridgeContract(opts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return nil, fmt.Errorf("no bridge contract")
}

func (b *polygonBridgePolygonERC20) bridgeContractAllowance(ctx context.Context) (*big.Int, error) {
	return nil, fmt.Errorf("no bridge contract")
}

func (b *polygonBridgePolygonERC20) requiresBridgeContractApproval() bool {
	return false
}

func (b *polygonBridgePolygonERC20) bridgeContractAddr() common.Address {
	return common.Address{}
}

func (b *polygonBridgePolygonERC20) initiateBridge(opts *bind.TransactOpts, destAssetID uint32, amount *big.Int) (*types.Transaction, error) {
	expectedDestAssetID := polygonBridgeSupportedAssets[b.net][b.assetID]
	if expectedDestAssetID != destAssetID {
		return nil, fmt.Errorf("%s cannot be bridged to %s", dex.BipIDSymbol(b.assetID), dex.BipIDSymbol(expectedDestAssetID))
	}

	tx, err := b.childERC20.Withdraw(opts, amount)
	return tx, err
}

func (b *polygonBridgePolygonERC20) getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error) {
	genURL := func(eventSignature string) string {
		var networkName string
		if b.net == dex.Mainnet {
			networkName = "matic"
		} else {
			networkName = "amoy"
		}
		return fmt.Sprintf("%s/api/v1/%s/exit-payload/%s?eventSignature=%s", proofGeneratorURL, networkName, bridgeTxID, eventSignature)
	}
	url := genURL(transferEventSignature)

	var res struct {
		Error   bool   `json:"error"`
		Message string `json:"message"`
		Result  string `json:"result"`
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := dexnet.Get(ctx, url, &res); err != nil {
		return nil, err
	} else if res.Error {
		return nil, fmt.Errorf("error: %s", res.Message)
	}

	return common.Hex2Bytes(strings.TrimPrefix(res.Result, "0x")), nil
}

func (b *polygonBridgePolygonERC20) requiresCompletion() bool {
	return false
}

func (b *polygonBridgePolygonERC20) supportedDestinations() []uint32 {
	return []uint32{polygonBridgeSupportedAssets[b.net][b.assetID]}
}

// verifyPolygonBridgeCompletion is called for bridges from ethereum to polygon
// POS, which do not require a completion transaction. It checks if the funds
// have been allocated on polygon POS. The initiation transaction contained a log
// with a state ID, and we check the LastStateId on the state receiver on polygon POS to
// see if it is equal or greater than the required state ID.
func verifyPolygonBridgeCompletion(ctx context.Context, cb bind.ContractBackend, stateReceiver *polygonbridge.StateReceiver, completionData []byte) (bool, error) {
	stateID := new(big.Int).SetBytes(completionData)

	callOpts := &bind.CallOpts{
		Context: ctx,
	}

	lastStateID, err := stateReceiver.LastStateId(callOpts)
	if err != nil {
		return false, err
	}

	return lastStateID.Cmp(stateID) >= 0, nil
}

func (b *polygonBridgePolygonERC20) verifyBridgeCompletion(ctx context.Context, completionData []byte) (bool, error) {
	return verifyPolygonBridgeCompletion(ctx, b.cb, b.stateReceiver, completionData)
}

func (b *polygonBridgePolygonERC20) completeBridge(opts *bind.TransactOpts, mintInfoB []byte) (*types.Transaction, error) {
	return nil, fmt.Errorf("no completion transaction is required when bridging from eth -> pol")
}

func (b *polygonBridgePolygonERC20) initiateBridgeGas() uint64 {
	return 60_000
}

func (b *polygonBridgePolygonERC20) completeBridgeGas() uint64 {
	return 0
}

type polygonBridgeAddresses struct {
	// rootChainManagerAddr is the address of the root chain manager contract.
	// This is the newer polygon bridge used to bridge ERC20s and ETH between
	// Ethereum and Polygon POS.
	rootChainManagerAddr common.Address
	// depositManagerAddr is the address of the deposit manager contract. This
	// is part of the original plasma bridge, and is used to deposit MATIC and
	// POL to Polygon POS.
	depositManagerAddr common.Address
}

var polygonBridgeAddrs = map[dex.Network]*polygonBridgeAddresses{
	dex.Mainnet: {
		rootChainManagerAddr: common.HexToAddress("0xA0c68C638235ee32657e8f720a23ceC1bFc77C77"),
		depositManagerAddr:   common.HexToAddress("0x401f6c983ea34274ec46f84d70b31c151321188b"),
	},
	dex.Testnet: {
		rootChainManagerAddr: common.HexToAddress("0x34f5a25b627f50bb3f5cab72807c4d4f405a9232"),
		depositManagerAddr:   common.HexToAddress("0x44Ad17990F9128C6d823Ee10dB7F0A5d40a731A4"),
	},
}

// polygonBridgeEth is used to manage the bridge operations on Ethereum
// for the native ETH asset.
type polygonBridgeEth struct {
	rootChainManager *polygonbridge.RootChainManager
	cb               bind.ContractBackend
	log              dex.Logger
	addr             common.Address
	net              dex.Network
	node             ethFetcher
}

var _ bridge = (*polygonBridgeEth)(nil)

func newPolygonBridgeEth(ctx context.Context, cb bind.ContractBackend, net dex.Network, addr common.Address, node ethFetcher, log dex.Logger) (*polygonBridgeEth, error) {
	addrs, found := polygonBridgeAddrs[net]
	if !found {
		return nil, fmt.Errorf("no root chain manager address found for network %s", net)
	}

	rootChainManager, err := polygonbridge.NewRootChainManager(addrs.rootChainManagerAddr, cb)
	if err != nil {
		return nil, err
	}

	return &polygonBridgeEth{
		rootChainManager: rootChainManager,
		cb:               cb,
		log:              log,
		addr:             addr,
		net:              net,
		node:             node,
	}, nil
}

func (b *polygonBridgeEth) bridgeContractAddr() common.Address {
	return common.Address{}
}

func (b *polygonBridgeEth) approveBridgeContract(opts *bind.TransactOpts, amt *big.Int) (*types.Transaction, error) {
	return nil, fmt.Errorf("no bridge contract")
}

func (b *polygonBridgeEth) bridgeContractAllowance(ctx context.Context) (*big.Int, error) {
	return nil, fmt.Errorf("no bridge contract")
}

func (b *polygonBridgeEth) requiresBridgeContractApproval() bool {
	return false
}

func (b *polygonBridgeEth) initiateBridge(opts *bind.TransactOpts, destAssetID uint32, amt *big.Int) (*types.Transaction, error) {
	expectedDestAssetID, _ := PolygonBridgeSupportedAsset(ethID, b.net)
	if expectedDestAssetID != destAssetID {
		return nil, fmt.Errorf("%s cannot be bridged to %s", dex.BipIDSymbol(ethID), dex.BipIDSymbol(expectedDestAssetID))
	}

	opts.Value = amt
	tx, err := b.rootChainManager.DepositEtherFor(opts, opts.From)
	return tx, err
}

func (b *polygonBridgeEth) getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error) {
	return getPolygonBridgeCompletionData(ctx, b.node, bridgeTxID)
}

func (b *polygonBridgeEth) requiresCompletion() bool {
	return true
}

func (b *polygonBridgeEth) completeBridge(opts *bind.TransactOpts, mintInfo []byte) (tx *types.Transaction, err error) {
	return b.rootChainManager.Exit(opts, mintInfo)
}

func (b *polygonBridgeEth) verifyBridgeCompletion(ctx context.Context, data []byte) (bool, error) {
	return false, fmt.Errorf("a completion transaction is required when bridging from eth -> pol")
}

func (b *polygonBridgeEth) initiateBridgeGas() uint64 {
	return 130_000
}

func (b *polygonBridgeEth) completeBridgeGas() uint64 {
	return 600_000
}

func (b *polygonBridgeEth) supportedDestinations() []uint32 {
	return []uint32{wethPolygonID}
}

// polygonBridgePolygonPOLToken is used to manage the bridge operations on
// Polygon for the native POL asset.
type polygonBridgePolygonPOLToken struct {
	rootChainManager *polygonbridge.RootChainManager
	cb               bind.ContractBackend
	log              dex.Logger
	addr             common.Address
	net              dex.Network
	stateReceiver    *polygonbridge.StateReceiver
}

var _ bridge = (*polygonBridgePolygonPOLToken)(nil)

func newPolygonBridgePolygonPOLToken(ctx context.Context, cb bind.ContractBackend, net dex.Network, addr common.Address, log dex.Logger) (*polygonBridgePolygonPOLToken, error) {
	addrs, found := polygonBridgeAddrs[net]
	if !found {
		return nil, fmt.Errorf("no root chain manager address found for network %s", net)
	}

	rootChainManager, err := polygonbridge.NewRootChainManager(addrs.rootChainManagerAddr, cb)
	if err != nil {
		return nil, err
	}

	stateReceiver, err := polygonbridge.NewStateReceiver(stateSyncAddress, cb)
	if err != nil {
		return nil, err
	}

	return &polygonBridgePolygonPOLToken{
		rootChainManager: rootChainManager,
		cb:               cb,
		log:              log,
		addr:             addr,
		net:              net,
		stateReceiver:    stateReceiver,
	}, nil
}

func (b *polygonBridgePolygonPOLToken) bridgeContractAddr() common.Address {
	return common.Address{}
}

func (b *polygonBridgePolygonPOLToken) approveBridgeContract(opts *bind.TransactOpts, amt *big.Int) (*types.Transaction, error) {
	return nil, fmt.Errorf("no bridge contract")
}

func (b *polygonBridgePolygonPOLToken) bridgeContractAllowance(ctx context.Context) (*big.Int, error) {
	return nil, fmt.Errorf("no bridge contract")
}

func (b *polygonBridgePolygonPOLToken) requiresBridgeContractApproval() bool {
	return false
}

func (b *polygonBridgePolygonPOLToken) initiateBridge(opts *bind.TransactOpts, destAssetID uint32, amt *big.Int) (*types.Transaction, error) {
	return nil, fmt.Errorf("not implemented")
}

func (b *polygonBridgePolygonPOLToken) getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

func (b *polygonBridgePolygonPOLToken) requiresCompletion() bool {
	return false
}

func (b *polygonBridgePolygonPOLToken) completeBridge(opts *bind.TransactOpts, mintInfo []byte) (tx *types.Transaction, err error) {
	return nil, fmt.Errorf("no completion transaction is required when bridging from eth -> pol")
}

func (b *polygonBridgePolygonPOLToken) verifyBridgeCompletion(ctx context.Context, data []byte) (bool, error) {
	return verifyPolygonBridgeCompletion(ctx, b.cb, b.stateReceiver, data)
}

func (b *polygonBridgePolygonPOLToken) initiateBridgeGas() uint64 {
	return 0
}

func (b *polygonBridgePolygonPOLToken) completeBridgeGas() uint64 {
	return 0
}

func (b *polygonBridgePolygonPOLToken) supportedDestinations() []uint32 {
	return []uint32{}
}

// polygonBridgeEthErc20 is the bridge operations on Ethereum for ERC20
// tokens.
type polygonBridgeEthErc20 struct {
	rootChainManager   *polygonbridge.RootChainManager
	tokenAddress       common.Address
	tokenContract      *erc20.IERC20
	erc20PredicateAddr common.Address
	cb                 bind.ContractBackend
	log                dex.Logger
	addr               common.Address
	assetID            uint32
	net                dex.Network
	node               ethFetcher
}

var _ bridge = (*polygonBridgeEthErc20)(nil)

func newPolygonBridgeEthErc20(ctx context.Context, cb bind.ContractBackend, assetID uint32, tokenAddress common.Address, net dex.Network, addr common.Address, node ethFetcher, log dex.Logger) (*polygonBridgeEthErc20, error) {
	addrs, found := polygonBridgeAddrs[net]
	if !found {
		return nil, fmt.Errorf("no root chain manager address found for network %s", net)
	}

	tokenContract, err := erc20.NewIERC20(tokenAddress, cb)
	if err != nil {
		return nil, err
	}

	rootChainManager, err := polygonbridge.NewRootChainManager(addrs.rootChainManagerAddr, cb)
	if err != nil {
		return nil, err
	}

	callOpts := &bind.CallOpts{
		From:    addr,
		Context: ctx,
	}

	tokenType, err := rootChainManager.TokenToType(callOpts, tokenAddress)
	if err != nil {
		return nil, err
	}

	erc20PredicateAddr, err := rootChainManager.TypeToPredicate(callOpts, tokenType)
	if err != nil {
		return nil, err
	}

	if erc20PredicateAddr == (common.Address{}) {
		return nil, fmt.Errorf("no erc20 predicate address found for token %s", tokenAddress.Hex())
	}

	return &polygonBridgeEthErc20{
		rootChainManager:   rootChainManager,
		tokenAddress:       tokenAddress,
		tokenContract:      tokenContract,
		erc20PredicateAddr: erc20PredicateAddr,
		cb:                 cb,
		log:                log,
		addr:               addr,
		assetID:            assetID,
		net:                net,
		node:               node,
	}, nil
}

func (b *polygonBridgeEthErc20) bridgeContractAddr() common.Address {
	return b.erc20PredicateAddr
}

func (b *polygonBridgeEthErc20) approveBridgeContract(opts *bind.TransactOpts, amt *big.Int) (*types.Transaction, error) {
	return b.tokenContract.Approve(opts, b.erc20PredicateAddr, amt)
}

func (b *polygonBridgeEthErc20) bridgeContractAllowance(ctx context.Context) (*big.Int, error) {
	_, pendingUnavailable := b.cb.(*multiRPCClient)
	callOpts := &bind.CallOpts{
		Pending: !pendingUnavailable,
		From:    b.addr,
		Context: ctx,
	}
	return b.tokenContract.Allowance(callOpts, b.addr, b.erc20PredicateAddr)
}

func (b *polygonBridgeEthErc20) requiresBridgeContractApproval() bool {
	return true
}

func (b *polygonBridgeEthErc20) verifyBridgeCompletion(ctx context.Context, data []byte) (bool, error) {
	return false, fmt.Errorf("a completion transaction is required when bridging from pol -> eth")
}

func (b *polygonBridgeEthErc20) initiateBridge(opts *bind.TransactOpts, destAssetID uint32, amt *big.Int) (*types.Transaction, error) {
	expectedDestAssetID, _ := PolygonBridgeSupportedAsset(b.assetID, b.net)
	if expectedDestAssetID != destAssetID {
		return nil, fmt.Errorf("%s cannot be bridged to %s", dex.BipIDSymbol(b.assetID), dex.BipIDSymbol(expectedDestAssetID))
	}

	depositData := make([]byte, 32)
	amtBytes := amt.Bytes()
	copy(depositData[32-len(amtBytes):], amtBytes)
	tx, err := b.rootChainManager.DepositFor(opts, opts.From, b.tokenAddress, depositData)
	return tx, err
}

// getPolygonBridgeCompletionData is called for bridges from ethereum to polygon
// POS. All deposit transactions will contain a log with the state ID. This will
// be used to check against the LastStateId on the state receiver on polygon POS.
// Once the lastStateId is greater than or equal to the state ID in the log, we
// can be sure that the funds have been allocated on polygon POS.
func getPolygonBridgeCompletionData(ctx context.Context, node ethFetcher, bridgeTxID string) ([]byte, error) {
	receipt, err := node.transactionReceipt(ctx, common.HexToHash(bridgeTxID))
	if err != nil {
		return nil, err
	}

	for _, log := range receipt.Logs {
		// This is the hash of the StateSynced event
		if log.Topics[0] == common.HexToHash("0x103fed9db65eac19c4d870f49ab7520fe03b99f1838e5996caf47e9e43308392") {
			if len(log.Topics) < 2 {
				return nil, fmt.Errorf("expected at least 2 topics, got %d", len(log.Topics))
			}
			return log.Topics[1].Bytes(), nil
		}
	}

	return nil, fmt.Errorf("no ID log found for bridge %s", bridgeTxID)
}

func (b *polygonBridgeEthErc20) getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error) {
	return getPolygonBridgeCompletionData(ctx, b.node, bridgeTxID)
}

func (b *polygonBridgeEthErc20) requiresCompletion() bool {
	return true
}

func (b *polygonBridgeEthErc20) completeBridge(opts *bind.TransactOpts, mintInfo []byte) (tx *types.Transaction, err error) {
	return b.rootChainManager.Exit(opts, mintInfo)
}

func (b *polygonBridgeEthErc20) initiateBridgeGas() uint64 {
	return 160_000
}

func (b *polygonBridgeEthErc20) completeBridgeGas() uint64 {
	return 600_000
}

func (b *polygonBridgeEthErc20) supportedDestinations() []uint32 {
	return []uint32{polygonBridgeSupportedAssets[b.net][b.assetID]}
}

// polygonBridgeEthPOLToken is the bridge operations on Ethereum for POL/MATIC
// tokens.
type polygonBridgeEthPOLToken struct {
	tokenAddress       common.Address
	tokenContract      *erc20.IERC20
	depositManager     *polygonbridge.DepositManager
	depositManagerAddr common.Address
	cb                 bind.ContractBackend
	log                dex.Logger
	addr               common.Address
	assetID            uint32
	net                dex.Network
	node               ethFetcher
}

var _ bridge = (*polygonBridgeEthPOLToken)(nil)

func newPolygonBridgeEthPOL(ctx context.Context, cb bind.ContractBackend, assetID uint32, tokenAddress common.Address, net dex.Network, addr common.Address, node ethFetcher, log dex.Logger) (*polygonBridgeEthPOLToken, error) {
	addrs, found := polygonBridgeAddrs[net]
	if !found {
		return nil, fmt.Errorf("no root chain manager address found for network %s", net)
	}

	tokenContract, err := erc20.NewIERC20(tokenAddress, cb)
	if err != nil {
		return nil, err
	}

	depositManager, err := polygonbridge.NewDepositManager(addrs.depositManagerAddr, cb)
	if err != nil {
		return nil, err
	}

	return &polygonBridgeEthPOLToken{
		tokenAddress:       tokenAddress,
		tokenContract:      tokenContract,
		depositManager:     depositManager,
		depositManagerAddr: addrs.depositManagerAddr,
		cb:                 cb,
		log:                log,
		addr:               addr,
		assetID:            assetID,
		net:                net,
		node:               node,
	}, nil
}

func (b *polygonBridgeEthPOLToken) bridgeContractAddr() common.Address {
	return b.depositManagerAddr
}

func (b *polygonBridgeEthPOLToken) approveBridgeContract(opts *bind.TransactOpts, amt *big.Int) (*types.Transaction, error) {
	return b.tokenContract.Approve(opts, b.depositManagerAddr, amt)
}

func (b *polygonBridgeEthPOLToken) bridgeContractAllowance(ctx context.Context) (*big.Int, error) {
	_, pendingUnavailable := b.cb.(*multiRPCClient)
	callOpts := &bind.CallOpts{
		Pending: !pendingUnavailable,
		From:    b.addr,
		Context: ctx,
	}
	return b.tokenContract.Allowance(callOpts, b.addr, b.depositManagerAddr)
}

func (b *polygonBridgeEthPOLToken) requiresBridgeContractApproval() bool {
	return true
}

func (b *polygonBridgeEthPOLToken) initiateBridge(opts *bind.TransactOpts, destAssetID uint32, amt *big.Int) (*types.Transaction, error) {
	expectedDestAssetID, _ := PolygonBridgeSupportedAsset(b.assetID, b.net)
	if expectedDestAssetID != destAssetID {
		return nil, fmt.Errorf("%s cannot be bridged to %s", dex.BipIDSymbol(b.assetID), dex.BipIDSymbol(expectedDestAssetID))
	}
	tx, err := b.depositManager.DepositERC20ForUser(opts, b.tokenAddress, b.addr, amt)
	return tx, err
}

func (b *polygonBridgeEthPOLToken) getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error) {
	return getPolygonBridgeCompletionData(ctx, b.node, bridgeTxID)
}

func (b *polygonBridgeEthPOLToken) requiresCompletion() bool {
	return false
}

func (b *polygonBridgeEthPOLToken) verifyBridgeCompletion(ctx context.Context, data []byte) (bool, error) {
	return false, fmt.Errorf("a completion transaction is required when bridging from pol -> eth")
}

func (b *polygonBridgeEthPOLToken) completeBridge(opts *bind.TransactOpts, mintInfo []byte) (tx *types.Transaction, err error) {
	panic("not implemented")
}

func (b *polygonBridgeEthPOLToken) initiateBridgeGas() uint64 {
	return 600_000
}

func (b *polygonBridgeEthPOLToken) completeBridgeGas() uint64 {
	panic("not implemented")
}

func (b *polygonBridgeEthPOLToken) supportedDestinations() []uint32 {
	return []uint32{polygonID}
}
