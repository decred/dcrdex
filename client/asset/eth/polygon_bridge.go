package eth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/networks/erc20"
	"decred.org/dcrdex/dex/networks/erc20/polygonbridge"
)

var (
	usdtEthID, _     = dex.BipSymbolID("usdt.eth")
	usdtPolygonID, _ = dex.BipSymbolID("usdt.polygon")
	wethPolygonID, _ = dex.BipSymbolID("weth.polygon")
	ethID, _         = dex.BipSymbolID("eth")
	polygonID, _     = dex.BipSymbolID("polygon")
	maticETHID, _    = dex.BipSymbolID("matic.eth")
)

var polygonBridgeSupportedAssets = map[dex.Network]map[uint32]bool{
	dex.Mainnet: {
		usdtEthID:     true,
		usdtPolygonID: true,
		ethID:         true,
		polygonID:     true,
		maticETHID:    true,
		wethPolygonID: true,
	},
	dex.Testnet: {
		ethID:         true,
		wethPolygonID: true,
		polygonID:     true,
		maticETHID:    true,
	},
}

var polBurnAddress = common.HexToAddress("0x0000000000000000000000000000000000001010")

const (
	transferEventSignature = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	withdrawEventSignature = "0xebff2602b3f468259e1e99f613fed6691f3a6526effe6ef3e768ba7ae7a36c4f"
	proofGeneratorURL      = "https://proof-generator.polygon.technology"
	healthCheckURL         = proofGeneratorURL + "/health-check"
)

func IsPolygonBridgeSupported(assetID uint32, net dex.Network) bool {
	supportedAssets, found := polygonBridgeSupportedAssets[net]
	if !found {
		return false
	}

	return supportedAssets[assetID]
}

// polygonBridgePolygon performs bridge operations on Polygon for both ERC20s and
// the native POL asset.
type polygonBridgePolygon struct {
	cb           bind.ContractBackend
	tokenAddress common.Address
	childERC20   *polygonbridge.ChildERC20
	log          dex.Logger
	assetID      uint32
	net          dex.Network
}

var _ bridge = (*polygonBridgePolygon)(nil)

func newPolygonBridgePolygonErc20(cb bind.ContractBackend, assetID uint32, tokenAddress common.Address, log dex.Logger, net dex.Network) (*polygonBridgePolygon, error) {
	childERC20, err := polygonbridge.NewChildERC20(tokenAddress, cb)
	if err != nil {
		return nil, err
	}
	return &polygonBridgePolygon{cb: cb, tokenAddress: tokenAddress, childERC20: childERC20, log: log, assetID: assetID, net: net}, nil
}

func newPolygonBridgePolygon(cb bind.ContractBackend, log dex.Logger, net dex.Network) (*polygonBridgePolygon, error) {
	return newPolygonBridgePolygonErc20(cb, polygonID, polBurnAddress, log, net)
}

func (b *polygonBridgePolygon) approveBridgeContract(opts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return nil, fmt.Errorf("no bridge contract")
}

func (b *polygonBridgePolygon) bridgeContractAllowance(ctx context.Context) (*big.Int, error) {
	return nil, fmt.Errorf("no bridge contract")
}

func (b *polygonBridgePolygon) requiresBridgeContractApproval() bool {
	return false
}

func (b *polygonBridgePolygon) bridgeContractAddr() common.Address {
	return common.Address{}
}

func (b *polygonBridgePolygon) initiateBridge(opts *bind.TransactOpts, destAssetID uint32, amount *big.Int) (*types.Transaction, bool, error) {
	if b.assetID == polygonID {
		opts.Value = amount
	}
	tx, err := b.childERC20.Withdraw(opts, amount)
	return tx, true, err
}

func (b *polygonBridgePolygon) getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error) {
	var url string

	genURL := func(eventSignature string) string {
		var networkName string
		if b.net == dex.Mainnet {
			networkName = "matic"
		} else {
			networkName = "amoy"
		}
		return fmt.Sprintf("%s/api/v1/%s/exit-payload/%s?eventSignature=%s", proofGeneratorURL, networkName, bridgeTxID, eventSignature)
	}

	// The proof generator API asks for a event signature where it can find out how much
	// is being withdrawn from polygon POS. A transfer event is emitted when withdrawing
	// ERC20s, and a withdraw event is emitted when withdrawing the native POL asset.
	if b.assetID == polygonID {
		url = genURL(withdrawEventSignature)
	} else {
		url = genURL(transferEventSignature)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch proof: %v", err)
	}
	defer resp.Body.Close()

	r, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read proof response: %v", err)
	}

	type result struct {
		Error   bool   `json:"error"`
		Message string `json:"message"`
		Result  string `json:"result"`
	}
	var res result
	if err := json.Unmarshal(r, &res); err != nil {
		return nil, err
	}
	if res.Error {
		return nil, fmt.Errorf("error: %s", res.Message)
	}

	return common.Hex2Bytes(strings.TrimPrefix(res.Result, "0x")), nil
}

func (b *polygonBridgePolygon) completeBridge(opts *bind.TransactOpts, mintInfoB []byte) (*types.Transaction, error) {
	return nil, fmt.Errorf("no completion transaction is required when bridging from eth -> pol")
}

func (b *polygonBridgePolygon) initiateBridgeGas() uint64 {
	return 60_000
}

func (b *polygonBridgePolygon) completeBridgeGas() uint64 {
	return 0
}

// polygonBridgeInfo contains addresses used by the Polygon bridge. These
// contracts are all on the Ethereum side.
type polygonBridgeInfo struct {
	// erc20PredicateAddr is the address of the contract that will spend
	// tokens when depositing from ETH to Polygon. This contract must be
	// approved to spend the user's tokens before the deposit can be made.
	erc20PredicateAddr common.Address
	// erc20PredicateBurnOnlyAddr is the contract used to redeem POL
	// tokens on the ETH side.
	erc20PredicateBurnOnlyAddr common.Address
	// rootChainManagerAddr is the address of the contract used to deposit
	// ETH to polygon, and withdrawing both ETH and ERC20s. Only the POL
	// token does not use this contract to withdraw.
	rootChainManagerAddr common.Address
}

var polygonBridgeInfos = map[dex.Network]*polygonBridgeInfo{
	dex.Mainnet: {
		erc20PredicateAddr:         common.HexToAddress("0x40ec5B33f54e0E8A33A975908C5BA1c14e5BbbDf"),
		erc20PredicateBurnOnlyAddr: common.HexToAddress("0x626fb210bf50e201ED62cA2705c16DE2a53DC966"),
		rootChainManagerAddr:       common.HexToAddress("0xA0c68C638235ee32657e8f720a23ceC1bFc77C77"),
	},
	dex.Testnet: {
		erc20PredicateAddr:         common.HexToAddress("0x4258c75b752c812b7fa586bdeb259f2d4bd17f4f"),
		erc20PredicateBurnOnlyAddr: common.HexToAddress("0x15EA6c538cF4b4A4f51999F433557285D5639820"),
		rootChainManagerAddr:       common.HexToAddress("0x34f5a25b627f50bb3f5cab72807c4d4f405a9232"),
	},
}

// polygonBridgeEth is used to manage the bridge operations on Ethereum
// for the native ETH asset.
type polygonBridgeEth struct {
	rootChainManager *polygonbridge.RootChainManager
	cb               bind.ContractBackend
	log              dex.Logger
	addr             common.Address
}

var _ bridge = (*polygonBridgeEth)(nil)

func newPolygonBridgeEth(cb bind.ContractBackend, net dex.Network, addr common.Address, log dex.Logger) (*polygonBridgeEth, error) {
	bridgeAddresses, found := polygonBridgeInfos[net]
	if !found {
		return nil, fmt.Errorf("no bridge info found for network %s", net)
	}

	rootChainManager, err := polygonbridge.NewRootChainManager(bridgeAddresses.rootChainManagerAddr, cb)
	if err != nil {
		return nil, err
	}

	return &polygonBridgeEth{
		rootChainManager: rootChainManager,
		cb:               cb,
		log:              log,
		addr:             addr,
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

func (b *polygonBridgeEth) initiateBridge(opts *bind.TransactOpts, destAssetID uint32, amt *big.Int) (*types.Transaction, bool, error) {
	opts.Value = amt
	tx, err := b.rootChainManager.DepositEtherFor(opts, opts.From)
	return tx, false, err
}

func (b *polygonBridgeEth) getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error) {
	return nil, fmt.Errorf("no completion transaction is required when bridging from eth -> pol")
}

func (b *polygonBridgeEth) completeBridge(opts *bind.TransactOpts, mintInfo []byte) (tx *types.Transaction, err error) {
	return b.rootChainManager.Exit(opts, mintInfo)
}

func (b *polygonBridgeEth) initiateBridgeGas() uint64 {
	return 130_000
}

func (b *polygonBridgeEth) completeBridgeGas() uint64 {
	return 600_000
}

// polygonBridgeEthErc20 is the bridge operations on Ethereum for ERC20
// tokens.
type polygonBridgeEthErc20 struct {
	rootChainManager   *polygonbridge.RootChainManager
	tokenAddress       common.Address
	tokenContract      *erc20.IERC20
	erc20PredicateAddr common.Address
	erc20BurnOnly      *polygonbridge.ERC20BurnOnlyPredicate
	cb                 bind.ContractBackend
	log                dex.Logger
	addr               common.Address
	assetID            uint32
}

var _ bridge = (*polygonBridgeEthErc20)(nil)

func newPolygonBridgeEthErc20(cb bind.ContractBackend, assetID uint32, tokenAddress common.Address, net dex.Network, addr common.Address, log dex.Logger) (*polygonBridgeEthErc20, error) {
	bridgeAddresses, found := polygonBridgeInfos[net]
	if !found {
		return nil, fmt.Errorf("no bridge info found for network %s", net)
	}

	tokenContract, err := erc20.NewIERC20(tokenAddress, cb)
	if err != nil {
		return nil, err
	}

	rootChainManager, err := polygonbridge.NewRootChainManager(bridgeAddresses.rootChainManagerAddr, cb)
	if err != nil {
		return nil, err
	}

	erc20BurnOnly, err := polygonbridge.NewERC20BurnOnlyPredicate(bridgeAddresses.erc20PredicateBurnOnlyAddr, cb)
	if err != nil {
		return nil, err
	}

	return &polygonBridgeEthErc20{
		rootChainManager:   rootChainManager,
		tokenAddress:       tokenAddress,
		tokenContract:      tokenContract,
		erc20PredicateAddr: bridgeAddresses.erc20PredicateAddr,
		erc20BurnOnly:      erc20BurnOnly,
		cb:                 cb,
		log:                log,
		addr:               addr,
		assetID:            assetID,
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

func (b *polygonBridgeEthErc20) initiateBridge(opts *bind.TransactOpts, destAssetID uint32, amt *big.Int) (*types.Transaction, bool, error) {
	depositData := make([]byte, 32)
	amtBytes := amt.Bytes()
	copy(depositData[32-len(amtBytes):], amtBytes)
	tx, err := b.rootChainManager.DepositFor(opts, opts.From, b.tokenAddress, depositData)
	return tx, true, err
}

func (b *polygonBridgeEthErc20) getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error) {
	return nil, fmt.Errorf("no completion transaction is required when bridging from eth -> pol")
}

func (b *polygonBridgeEthErc20) completeBridge(opts *bind.TransactOpts, mintInfo []byte) (tx *types.Transaction, err error) {
	if b.assetID == maticETHID {
		return b.erc20BurnOnly.StartExitWithBurntTokens(opts, mintInfo)
	}
	return b.rootChainManager.Exit(opts, mintInfo)
}

func (b *polygonBridgeEthErc20) initiateBridgeGas() uint64 {
	return 160_000
}

func (b *polygonBridgeEthErc20) completeBridgeGas() uint64 {
	return 600_000
}
