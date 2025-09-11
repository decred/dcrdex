package eth

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"decred.org/dcrdex/client/asset"
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
	polygonEthID, _  = dex.BipSymbolID("polygon.eth")
	maticEthID, _    = dex.BipSymbolID("matic.eth")
)

// network -> chainAssetID -> sourceAssetID -> destAssetID
var polygonBridgeSupportedAssets = map[dex.Network]map[uint32]map[uint32]uint32{
	dex.Mainnet: {
		ethID: {
			usdtEthID:    usdtPolygonID,
			ethID:        wethPolygonID,
			maticEthID:   polygonID,
			polygonEthID: polygonID,
		},
		polygonID: {
			usdtPolygonID: usdtEthID,
			wethPolygonID: ethID,
			polygonID:     polygonEthID,
		},
	},
	dex.Testnet: {
		ethID: {
			ethID:        wethPolygonID,
			polygonEthID: polygonID,
		},
		polygonID: {
			wethPolygonID: ethID,
			polygonID:     polygonEthID,
		},
	},
}

var polygonBurnAddress = common.HexToAddress("0x0000000000000000000000000000000000001010")
var stateSyncAddress = common.HexToAddress("0x0000000000000000000000000000000000001001")

const (
	transferEventSignature = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	withdrawEventSignature = "0xebff2602b3f468259e1e99f613fed6691f3a6526effe6ef3e768ba7ae7a36c4f"
	proofGeneratorURL      = "https://proof-generator.polygon.technology"
	healthCheckURL         = proofGeneratorURL + "/health-check"
)

// polygonBridgePolygon performs bridge operations on the polygon POS chain.
type polygonBridgePolygon struct {
	cb            bind.ContractBackend
	stateReceiver *polygonbridge.StateReceiver
	log           dex.Logger
	net           dex.Network
}

var _ bridge = (*polygonBridgePolygon)(nil)

func newPolygonBridgePolygon(cb bind.ContractBackend, net dex.Network, log dex.Logger) (*polygonBridgePolygon, error) {
	stateReceiver, err := polygonbridge.NewStateReceiver(stateSyncAddress, cb)
	if err != nil {
		return nil, err
	}

	return &polygonBridgePolygon{cb: cb, stateReceiver: stateReceiver, log: log, net: net}, nil
}

func (b *polygonBridgePolygon) approveBridgeContract(opts *bind.TransactOpts, amount *big.Int, assetID uint32) (*types.Transaction, error) {
	return nil, fmt.Errorf("no bridge contract for polygon withdrawals")
}

func (b *polygonBridgePolygon) bridgeContractAllowance(ctx context.Context, assetID uint32) (*big.Int, error) {
	return nil, fmt.Errorf("no bridge contract for polygon withdrawals")
}

func (b *polygonBridgePolygon) requiresBridgeContractApproval(uint32) bool {
	return false
}

func (b *polygonBridgePolygon) bridgeContractAddr(ctx context.Context, assetID uint32) (common.Address, error) {
	return common.Address{}, fmt.Errorf("no bridge contract for polygon withdrawals")
}

func (b *polygonBridgePolygon) initiateBridge(opts *bind.TransactOpts, sourceAssetID, destAssetID uint32, amount *big.Int) (*types.Transaction, error) {
	expectedDestAssetID := polygonBridgeSupportedAssets[b.net][polygonID][sourceAssetID]
	if expectedDestAssetID != destAssetID {
		return nil, fmt.Errorf("%s cannot be bridged to %s", dex.BipIDSymbol(sourceAssetID), dex.BipIDSymbol(destAssetID))
	}

	if sourceAssetID == polygonID {
		burnContract, err := polygonbridge.NewChildERC20(polygonBurnAddress, b.cb)
		if err != nil {
			return nil, err
		}
		opts.Value = amount
		return burnContract.Withdraw(opts, amount)
	}

	tokenInfo := asset.TokenInfo(sourceAssetID)
	if tokenInfo == nil {
		return nil, fmt.Errorf("token info not found for assetID %d", sourceAssetID)
	}
	contractAddress := common.HexToAddress(tokenInfo.ContractAddress)
	childERC20, err := polygonbridge.NewChildERC20(contractAddress, b.cb)
	if err != nil {
		return nil, err
	}
	return childERC20.Withdraw(opts, amount)
}

func getPolygonWithdrawalCompletionData(ctx context.Context, bridgeTxID string, isToken bool, network dex.Network) ([]byte, error) {
	genURL := func(eventSignature string) string {
		var networkName string
		if network == dex.Mainnet {
			networkName = "matic"
		} else {
			networkName = "amoy"
		}
		return fmt.Sprintf("%s/api/v1/%s/exit-payload/%s?eventSignature=%s", proofGeneratorURL, networkName, bridgeTxID, eventSignature)
	}
	var url string
	if isToken {
		url = genURL(transferEventSignature)
	} else {
		url = genURL(withdrawEventSignature)
	}

	var res struct {
		Error   bool   `json:"error"`
		Message string `json:"message"`
		Result  string `json:"result,omitempty"`
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := dexnet.Get(ctx, url, &res, dexnet.WithErrorParsing(&res)); err != nil {
		if res.Message == "Burn transaction has not been checkpointed yet" {
			return nil, nil
		}
		return nil, err
	} else if res.Error {
		return nil, fmt.Errorf("error: %s", res.Message)
	}

	return common.Hex2Bytes(strings.TrimPrefix(res.Result, "0x")), nil
}

func (b *polygonBridgePolygon) getCompletionData(ctx context.Context, sourceAssetID uint32, bridgeTxID string) ([]byte, error) {
	isToken := sourceAssetID != polygonID
	return getPolygonWithdrawalCompletionData(ctx, bridgeTxID, isToken, b.net)
}

func (b *polygonBridgePolygon) requiresCompletion(uint32) bool {
	return false
}

func (b *polygonBridgePolygon) requiresFollowUpCompletion(uint32) bool {
	return false
}

func (b *polygonBridgePolygon) getFollowUpCompletionData(ctx context.Context, completionTxID string) (required bool, data []byte, err error) {
	return false, nil, fmt.Errorf("not implemented")
}

func (b *polygonBridgePolygon) completeFollowUpBridge(txOpts *bind.TransactOpts, data []byte) (tx *types.Transaction, err error) {
	return nil, fmt.Errorf("not implemented")
}

func (b *polygonBridgePolygon) followUpCompleteBridgeGas() uint64 {
	return 0
}

func (b *polygonBridgePolygon) supportedDestinations(sourceAssetID uint32) []uint32 {
	if dest, found := polygonBridgeSupportedAssets[b.net][polygonID][sourceAssetID]; found {
		return []uint32{dest}
	}
	return nil
}

func (b *polygonBridgePolygon) bridgeLimits(sourceAssetID, destAssetID uint32) (*big.Int, *big.Int, bool, error) {
	// Polygon bridge doesn't have limits
	return big.NewInt(0), big.NewInt(0), false, nil
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

func (b *polygonBridgePolygon) verifyBridgeCompletion(ctx context.Context, completionData []byte) (bool, error) {
	return verifyPolygonBridgeCompletion(ctx, b.cb, b.stateReceiver, completionData)
}

func (b *polygonBridgePolygon) completeBridge(opts *bind.TransactOpts, destAssetID uint32, mintInfoB []byte) (*types.Transaction, error) {
	return nil, fmt.Errorf("no completion transaction is required when bridging from eth -> pol")
}

func (b *polygonBridgePolygon) initiateBridgeGas(uint32) uint64 {
	return 60_000
}

func (b *polygonBridgePolygon) completeBridgeGas(uint32) uint64 {
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
	depositManagerAddr         common.Address
	withdrawManagerAddr        common.Address
	erc20PredicateBurnOnlyAddr common.Address
}

var polygonBridgeAddrs = map[dex.Network]*polygonBridgeAddresses{
	dex.Mainnet: {
		rootChainManagerAddr:       common.HexToAddress("0xA0c68C638235ee32657e8f720a23ceC1bFc77C77"),
		depositManagerAddr:         common.HexToAddress("0x401f6c983ea34274ec46f84d70b31c151321188b"),
		withdrawManagerAddr:        common.HexToAddress("0x2A88696e0fFA76bAA1338F2C74497cC013495922"),
		erc20PredicateBurnOnlyAddr: common.HexToAddress("0x626fb210bf50e201ed62ca2705c16de2a53dc966"),
	},
	dex.Testnet: {
		rootChainManagerAddr:       common.HexToAddress("0x34f5a25b627f50bb3f5cab72807c4d4f405a9232"),
		depositManagerAddr:         common.HexToAddress("0x44Ad17990F9128C6d823Ee10dB7F0A5d40a731A4"),
		withdrawManagerAddr:        common.HexToAddress("0x822db7e79096E7247d9273E5782ecAec464Eb96C"),
		erc20PredicateBurnOnlyAddr: common.HexToAddress("0x15EA6c538cF4b4A4f51999F433557285D5639820"),
	},
}

// polygonBridgeEth is used to manage the bridge operations on Ethereum.
type polygonBridgeEth struct {
	rootChainManager       *polygonbridge.RootChainManager
	cb                     bind.ContractBackend
	log                    dex.Logger
	addr                   common.Address
	net                    dex.Network
	node                   ethFetcher
	withdrawManager        *polygonbridge.WithdrawManager
	exitNFT                *polygonbridge.ExitNFT
	erc20PredicateBurnOnly *polygonbridge.Erc20PredicateBurnOnly
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

	withdrawManager, err := polygonbridge.NewWithdrawManager(addrs.withdrawManagerAddr, cb)
	if err != nil {
		return nil, err
	}

	exitNFTAddr, err := withdrawManager.ExitNft(&bind.CallOpts{})
	if err != nil {
		return nil, err
	}

	exitNFT, err := polygonbridge.NewExitNFT(exitNFTAddr, cb)
	if err != nil {
		return nil, err
	}

	erc20PredicateBurnOnly, err := polygonbridge.NewErc20PredicateBurnOnly(addrs.erc20PredicateBurnOnlyAddr, cb)
	if err != nil {
		return nil, err
	}

	return &polygonBridgeEth{
		rootChainManager:       rootChainManager,
		cb:                     cb,
		log:                    log,
		addr:                   addr,
		net:                    net,
		node:                   node,
		withdrawManager:        withdrawManager,
		exitNFT:                exitNFT,
		erc20PredicateBurnOnly: erc20PredicateBurnOnly,
	}, nil
}

func (b *polygonBridgeEth) bridgeContractAddr(ctx context.Context, assetID uint32) (common.Address, error) {
	if polygonBridgeSupportedAssets[b.net][ethID][assetID] == 0 {
		return common.Address{}, fmt.Errorf("%d is not supported by the polygon bridge", assetID)
	}

	// ETH does not require a bridge contract approval
	if assetID == ethID {
		return common.Address{}, fmt.Errorf("eth does not require a bridge contract approval")
	}

	// MATIC and POL have their own special bridge contract address
	if assetID == maticEthID || assetID == polygonEthID {
		polygonBridgeAddrs, found := polygonBridgeAddrs[b.net]
		if !found {
			return common.Address{}, fmt.Errorf("no root chain manager address found for network %s", b.net)
		}
		return polygonBridgeAddrs.depositManagerAddr, nil
	}

	// Each other ERC20 address has a corresponding erc20Predicate address
	tokenInfo := asset.TokenInfo(assetID)
	if tokenInfo == nil {
		return common.Address{}, fmt.Errorf("token info not found for assetID %d", assetID)
	}
	tokenAddress := common.HexToAddress(tokenInfo.ContractAddress)
	callOpts := &bind.CallOpts{
		From:    b.addr,
		Context: ctx,
	}
	tokenType, err := b.rootChainManager.TokenToType(callOpts, tokenAddress)
	if err != nil {
		return common.Address{}, err
	}
	erc20PredicateAddr, err := b.rootChainManager.TypeToPredicate(callOpts, tokenType)
	if err != nil {
		return common.Address{}, err
	}
	if erc20PredicateAddr == (common.Address{}) {
		return common.Address{}, fmt.Errorf("no erc20 predicate address found for token %s", tokenAddress.Hex())
	}

	return erc20PredicateAddr, nil
}

func (b *polygonBridgeEth) approveBridgeContract(txOpts *bind.TransactOpts, amount *big.Int, assetID uint32) (*types.Transaction, error) {
	contractAddr, err := b.bridgeContractAddr(txOpts.Context, assetID)
	if err != nil {
		return nil, err
	}

	tokenInfo := asset.TokenInfo(assetID)
	if tokenInfo == nil {
		return nil, fmt.Errorf("token info not found for assetID %d", assetID)
	}

	tokenAddress := common.HexToAddress(tokenInfo.ContractAddress)
	tokenContract, err := erc20.NewIERC20(tokenAddress, b.cb)
	if err != nil {
		return nil, err
	}

	return tokenContract.Approve(txOpts, contractAddr, amount)
}

func (b *polygonBridgeEth) bridgeContractAllowance(ctx context.Context, assetID uint32) (*big.Int, error) {
	contractAddr, err := b.bridgeContractAddr(ctx, assetID)
	if err != nil {
		return nil, err
	}

	tokenInfo := asset.TokenInfo(assetID)
	if tokenInfo == nil {
		return nil, fmt.Errorf("token info not found for assetID %d", assetID)
	}

	tokenAddress := common.HexToAddress(tokenInfo.ContractAddress)
	tokenContract, err := erc20.NewIERC20(tokenAddress, b.cb)
	if err != nil {
		return nil, err
	}

	_, pendingUnavailable := b.cb.(*multiRPCClient)
	callOpts := &bind.CallOpts{
		Pending: !pendingUnavailable,
		From:    b.addr,
		Context: ctx,
	}

	return tokenContract.Allowance(callOpts, b.addr, contractAddr)
}

func (b *polygonBridgeEth) requiresBridgeContractApproval(assetID uint32) bool {
	return assetID != ethID
}

func (b *polygonBridgeEth) initiateBridge(opts *bind.TransactOpts, sourceAssetID, destAssetID uint32, amt *big.Int) (*types.Transaction, error) {
	expectedDestAssetID := polygonBridgeSupportedAssets[b.net][ethID][sourceAssetID]
	if expectedDestAssetID != destAssetID {
		return nil, fmt.Errorf("%s cannot be bridged to %s", dex.BipIDSymbol(sourceAssetID), dex.BipIDSymbol(destAssetID))
	}

	if sourceAssetID == ethID {
		opts.Value = amt
		return b.rootChainManager.DepositEtherFor(opts, opts.From)
	}

	if sourceAssetID == maticEthID || sourceAssetID == polygonEthID {
		polygonBridgeAddrs, found := polygonBridgeAddrs[b.net]
		if !found {
			return nil, fmt.Errorf("no root chain manager address found for network %s", b.net)
		}
		depositManager, err := polygonbridge.NewDepositManager(polygonBridgeAddrs.depositManagerAddr, b.cb)
		if err != nil {
			return nil, err
		}
		tokenInfo := asset.TokenInfo(sourceAssetID)
		if tokenInfo == nil {
			return nil, fmt.Errorf("token info not found for assetID %d", sourceAssetID)
		}
		tokenAddress := common.HexToAddress(tokenInfo.ContractAddress)

		return depositManager.DepositERC20ForUser(opts, tokenAddress, b.addr, amt)
	}

	// All other ERC20s
	depositData := make([]byte, 32)
	amtBytes := amt.Bytes()
	copy(depositData[32-len(amtBytes):], amtBytes)

	tokenInfo := asset.TokenInfo(sourceAssetID)
	if tokenInfo == nil {
		return nil, fmt.Errorf("token info not found for assetID %d", sourceAssetID)
	}
	tokenAddress := common.HexToAddress(tokenInfo.ContractAddress)
	return b.rootChainManager.DepositFor(opts, opts.From, tokenAddress, depositData)
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

func (b *polygonBridgeEth) getCompletionData(ctx context.Context, sourceAssetID uint32, bridgeTxID string) ([]byte, error) {
	return getPolygonBridgeCompletionData(ctx, b.node, bridgeTxID)
}

func (b *polygonBridgeEth) requiresCompletion(uint32) bool {
	return true
}

func (b *polygonBridgeEth) completeBridge(opts *bind.TransactOpts, destAssetID uint32, mintInfo []byte) (*types.Transaction, error) {
	if destAssetID == polygonEthID || destAssetID == maticEthID {
		return b.erc20PredicateBurnOnly.StartExitWithBurntTokens(opts, mintInfo)
	}
	return b.rootChainManager.Exit(opts, mintInfo)
}

func (b *polygonBridgeEth) initiateBridgeGas(sourceAssetID uint32) uint64 {
	if sourceAssetID == ethID {
		return 130_000
	}
	if sourceAssetID == maticEthID {
		return 600_000
	}

	// All other ERC20s
	return 160_000
}

func (b *polygonBridgeEth) completeBridgeGas(uint32) uint64 {
	return 600_000
}

func (b *polygonBridgeEth) requiresFollowUpCompletion(destAssetID uint32) bool {
	return destAssetID == polygonEthID || destAssetID == maticEthID
}

type polWithdrawalFollowUpData struct {
	ExitID *big.Int       `json:"exitId"`
	Token  common.Address `json:"token"`
}

func (b *polygonBridgeEth) getFollowUpCompletionData(ctx context.Context, completionTxID string) (required bool, data []byte, err error) {
	receipt, err := b.node.transactionReceipt(ctx, common.HexToHash(completionTxID))
	if err != nil {
		return false, nil, err
	}

	for _, log := range receipt.Logs {
		if log.Topics[0] == common.HexToHash("0xaa5303fdad123ab5ecaefaf69137bf8632257839546d43a3b3dd148cc2879d6f") {
			if len(log.Topics) < 3 {
				return false, nil, fmt.Errorf("expected at least 2 topics, got %d", len(log.Topics))
			}
			exitID := new(big.Int).SetBytes(log.Topics[2].Bytes())
			token := common.HexToAddress(log.Topics[3].Hex())

			data, err := json.Marshal(polWithdrawalFollowUpData{ExitID: exitID, Token: token})
			if err != nil {
				return false, nil, err
			}

			bridgeComplete, err := b.verifyBridgeCompletion(ctx, data)
			if err != nil {
				return false, nil, err
			}

			return !bridgeComplete, data, nil
		}
	}

	return false, nil, fmt.Errorf("no exit log found for completion tx %s", completionTxID)
}

func (b *polygonBridgeEth) verifyBridgeCompletion(ctx context.Context, data []byte) (bool, error) {
	followUpData := polWithdrawalFollowUpData{}
	err := json.Unmarshal(data, &followUpData)
	if err != nil {
		return false, err
	}

	callOpts := &bind.CallOpts{
		Context: ctx,
	}

	exists, err := b.exitNFT.Exists(callOpts, followUpData.ExitID)
	if err != nil {
		return false, err
	}

	// If the exit has been deleted, it means the bridge is complete
	return !exists, nil
}

func (b *polygonBridgeEth) completeFollowUpBridge(txOpts *bind.TransactOpts, data []byte) (tx *types.Transaction, err error) {
	followUpData := polWithdrawalFollowUpData{}
	err = json.Unmarshal(data, &followUpData)
	if err != nil {
		return nil, err
	}

	return b.withdrawManager.ProcessExits(txOpts, followUpData.Token)
}

func (b *polygonBridgeEth) followUpCompleteBridgeGas() uint64 {
	return 600_000
}

func (b *polygonBridgeEth) supportedDestinations(sourceAssetID uint32) []uint32 {
	if dest, found := polygonBridgeSupportedAssets[b.net][ethID][sourceAssetID]; found {
		return []uint32{dest}
	}
	return nil
}

func (b *polygonBridgeEth) bridgeLimits(sourceAssetID, destAssetID uint32) (*big.Int, *big.Int, bool, error) {
	// Polygon bridge doesn't have limits
	return big.NewInt(0), big.NewInt(0), false, nil
}
