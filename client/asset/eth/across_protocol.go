package eth

import (
	"context"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/dexnet"
	"decred.org/dcrdex/dex/networks/erc20"

	"decred.org/dcrdex/dex/networks/erc20/across"
)

var (
	chainAssetIDToAcrossChainID = map[dex.Network]map[uint32]uint64{
		dex.Mainnet: {
			ethID:     1,
			polygonID: 137,
			baseID:    8453,
		},
		dex.Testnet: {
			ethID:     11155111,
			polygonID: 80002,
			baseID:    84532,
		},
	}

	// Reverse of chainSuffixToID. Populated in init().
	idToChainAssetID = map[dex.Network]map[uint64]uint32{
		dex.Mainnet: make(map[uint64]uint32),
		dex.Testnet: make(map[uint64]uint32),
	}

	acrossSpokePoolAddrs = map[dex.Network]map[uint64]common.Address{
		dex.Mainnet: {
			1:    common.HexToAddress("0x5c7BCd6E7De5423a257D81B442095A1a6ced35C5"),
			137:  common.HexToAddress("0x9295ee1d8C5b022Be115A2AD3c30C72E34e7F096"),
			8453: common.HexToAddress("0x09aea4b2242abC8bb4BB78D537A67a245A7bEC64"),
		},
		dex.Testnet: {
			11155111: common.HexToAddress("0x5ef6C01E11889d86803e0B23e3cB3F9E9d97B662"),
			80002:    common.HexToAddress("0xd08baaE74D6d2eAb1F3320B2E1a53eeb391ce8e5"),
			84532:    common.HexToAddress("0x82B564983aE7274c86695917BBf8C99ECb6F0F8F"),
		},
	}

	// native token addresses for each chain. i.e. weth on ethereum.
	chainIDToNativeTokenAddr = map[dex.Network]map[uint64]common.Address{
		dex.Mainnet: {
			1:    common.HexToAddress("0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"),
			8453: common.HexToAddress("0x4200000000000000000000000000000000000006"),
		},
		dex.Testnet: {
			11155111: common.HexToAddress("0xfFf9976782d46CC05630D1f6eBAb18b2324d6B14"),
			84532:    common.HexToAddress("0x4200000000000000000000000000000000000006"),
		},
	}

	// Global cache for across available routes.
	acrossAvailableRoutesCache = make(map[dex.Network][]acrossAvailableRoute)
	acrossAvailableRoutesMutex sync.Mutex
)

func init() {
	for net, chainAssetIDToAcrossChainID := range chainAssetIDToAcrossChainID {
		for chainAssetID, acrossChainID := range chainAssetIDToAcrossChainID {
			idToChainAssetID[net][acrossChainID] = chainAssetID
		}
	}
}

func AcrossBridgeSupportedAsset(assetID uint32, net dex.Network) (supported bool) {
	return assetIDToAcrossAsset(net, assetID) != nil
}

func acrossBaseURL(net dex.Network) string {
	if net == dex.Mainnet {
		return "https://app.across.to"
	}
	return "https://testnet.across.to"
}

func getSpokePoolAddr(net dex.Network, chainID uint64) (common.Address, error) {
	addrs, found := acrossSpokePoolAddrs[net]
	if !found {
		return common.Address{}, fmt.Errorf("no spoke pool addresses for network %s", net)
	}
	spokePoolAddr, found := addrs[chainID]
	if !found {
		return common.Address{}, fmt.Errorf("no spoke pool for chain %d", chainID)
	}
	return spokePoolAddr, nil
}

type acrossAsset struct {
	chainID uint64
	symbol  string
	address common.Address
}

func assetIDToAcrossAsset(net dex.Network, assetID uint32) *acrossAsset {
	symbol := dex.BipIDSymbol(assetID)
	parts := strings.Split(symbol, ".")
	assetName := parts[0]
	chainName := parts[len(parts)-1]

	chainAssetID, found := dex.BipSymbolID(chainName)
	if !found {
		return nil
	}

	chainID, found := chainAssetIDToAcrossChainID[net][chainAssetID]
	if !found {
		return nil
	}

	var address common.Address
	if assetName != chainName {
		token := asset.TokenInfo(assetID)
		if token == nil {
			return nil
		}
		address = common.HexToAddress(token.ContractAddress)
	} else {
		address = chainIDToNativeTokenAddr[net][chainID]
	}

	// The weth address should be provided for ETH, but the protocol will unwrap
	// the weth for EOA accounts.
	if assetName == "eth" {
		assetName = "weth"
	}
	if assetName == "base" {
		assetName = "weth"
	}

	return &acrossAsset{chainID: chainID, symbol: strings.ToUpper(assetName), address: address}
}

func acrossAssetToAssetID(net dex.Network, symbol string, chainID uint64) (uint32, bool) {
	chainAssetID, found := idToChainAssetID[net][chainID]
	if !found {
		return 0, false
	}

	chainSymbol := dex.BipIDSymbol(chainAssetID)
	assetSymbol := strings.ToLower(symbol)
	fullSymbol := assetSymbol
	if chainSymbol != assetSymbol {
		fullSymbol = fmt.Sprintf("%s.%s", assetSymbol, chainSymbol)
	}

	if fullSymbol == "weth.eth" {
		fullSymbol = "eth"
	}
	if fullSymbol == "weth.base" {
		fullSymbol = "base"
	}

	return dex.BipSymbolID(fullSymbol)
}

// acrossAvailableRoute is the response structure for the Across available-routes API.
type acrossAvailableRoute struct {
	OriginChainID          int    `json:"originChainId"`
	DestinationChainID     int    `json:"destinationChainId"`
	OriginToken            string `json:"originToken"`
	DestinationToken       string `json:"destinationToken"`
	OriginTokenSymbol      string `json:"originTokenSymbol"`
	DestinationTokenSymbol string `json:"destinationTokenSymbol"`
	IsNative               bool   `json:"isNative"`
}

// getAcrossAvailableRoutes fetches available routes from the across API.
// We make this same call for all wallets, so to avoid making too many calls
// we cache the results.
func getAcrossAvailableRoutes(ctx context.Context, net dex.Network) ([]acrossAvailableRoute, error) {
	acrossAvailableRoutesMutex.Lock()
	defer acrossAvailableRoutesMutex.Unlock()

	if routes, exists := acrossAvailableRoutesCache[net]; exists {
		return routes, nil
	}

	url := fmt.Sprintf("%s/api/available-routes", acrossBaseURL(net))
	var routes []acrossAvailableRoute
	if err := dexnet.Get(ctx, url, &routes); err != nil {
		return nil, fmt.Errorf("failed to fetch available routes: %w", err)
	}

	acrossAvailableRoutesCache[net] = routes

	return routes, nil
}

// returns the list of asset IDs that are supported as destinations for the origin asset.
func acrossSupportedDestinations(ctx context.Context, assetID uint32, net dex.Network, log dex.Logger) (destinations []uint32, err error) {
	res, err := getAcrossAvailableRoutes(ctx, net)
	if err != nil {
		return nil, err
	}

	originAsset := assetIDToAcrossAsset(net, assetID)
	if originAsset == nil {
		return nil, fmt.Errorf("unknown across asset %d", assetID)
	}

	for _, route := range res {
		// Check if the origin asset of the route matches the origin asset.
		if originAsset.chainID != uint64(route.OriginChainID) || originAsset.symbol != route.OriginTokenSymbol {
			continue
		}

		if common.HexToAddress(route.OriginToken) != originAsset.address {
			return nil, fmt.Errorf("token info mismatch for asset %d: %s != %s", assetID, route.OriginToken, originAsset.address.Hex())
		}

		// Check if the asset is supported by Bison Wallet.
		destAssetID, found := acrossAssetToAssetID(net, route.DestinationTokenSymbol, uint64(route.DestinationChainID))
		if !found {
			continue
		}

		destAsset := assetIDToAcrossAsset(net, destAssetID)
		if destAsset == nil {
			continue
		}

		if common.HexToAddress(route.DestinationToken) != destAsset.address {
			log.Errorf("token info mismatch for asset %d: %s != %s", destAssetID, route.DestinationToken, destAsset.address.Hex())
			continue
		}

		destinations = append(destinations, destAssetID)
	}

	return destinations, nil
}

// acrossBridge implements the bridge interface for the Across protocol.
type acrossBridge struct {
	cb            bind.ContractBackend
	spokePool     *across.SpokePool
	spokePoolAddr common.Address
	log           dex.Logger
	net           dex.Network
	chainAssetID  uint32
	chainID       uint64
	addr          common.Address
	node          ethFetcher
	apiBaseURL    string
}

var _ bridge = (*acrossBridge)(nil)

func newAcrossBridge(ctx context.Context, cb bind.ContractBackend, node ethFetcher, chainAssetID uint32, net dex.Network, walletAddr common.Address, log dex.Logger) (*acrossBridge, error) {
	chainID, found := chainAssetIDToAcrossChainID[net][chainAssetID]
	if !found {
		return nil, fmt.Errorf("unknown chain asset ID %d", chainAssetID)
	}
	spokePoolAddr, err := getSpokePoolAddr(net, chainID)
	if err != nil {
		return nil, err
	}
	spokePool, err := across.NewSpokePool(spokePoolAddr, cb)
	if err != nil {
		return nil, err
	}

	apiBaseURL := acrossBaseURL(net)

	return &acrossBridge{
		cb:            cb,
		spokePool:     spokePool,
		spokePoolAddr: spokePoolAddr,
		log:           log,
		net:           net,
		chainAssetID:  chainAssetID,
		chainID:       chainID,
		addr:          walletAddr,
		node:          node,
		apiBaseURL:    apiBaseURL,
	}, nil
}

func (b *acrossBridge) bridgeContractAddr(ctx context.Context, sourceAssetID uint32) (common.Address, error) {
	if sourceAssetID == b.chainAssetID {
		return common.Address{}, fmt.Errorf("no bridge contract approval required for native asset on chain")
	}

	return b.spokePoolAddr, nil
}

func (b *acrossBridge) bridgeContractAllowance(ctx context.Context, sourceAssetID uint32) (*big.Int, error) {
	if sourceAssetID == b.chainAssetID {
		return nil, fmt.Errorf("no bridge contract approval required for native asset on chain")
	}

	tokenInfo := asset.TokenInfo(sourceAssetID)
	if tokenInfo == nil {
		return nil, fmt.Errorf("no token info for asset %d", sourceAssetID)
	}
	tokenAddr := common.HexToAddress(tokenInfo.ContractAddress)
	tokenContract, err := erc20.NewIERC20(tokenAddr, b.cb)
	if err != nil {
		return nil, err
	}

	_, pendingUnavailable := b.cb.(*multiRPCClient)
	callOpts := &bind.CallOpts{
		Pending: !pendingUnavailable,
		From:    b.addr,
		Context: ctx,
	}

	return tokenContract.Allowance(callOpts, b.addr, b.spokePoolAddr)
}

func (b *acrossBridge) approveBridgeContract(txOpts *bind.TransactOpts, amount *big.Int, sourceAssetID uint32) (*types.Transaction, error) {
	if sourceAssetID == b.chainAssetID {
		return nil, fmt.Errorf("no bridge contract approval required for native asset on chain")
	}

	tokenInfo := asset.TokenInfo(sourceAssetID)
	if tokenInfo == nil {
		return nil, fmt.Errorf("no token info for asset %d", sourceAssetID)
	}
	tokenAddr := common.HexToAddress(tokenInfo.ContractAddress)
	tokenContract, err := erc20.NewIERC20(tokenAddr, b.cb)
	if err != nil {
		return nil, err
	}

	return tokenContract.Approve(txOpts, b.spokePoolAddr, amount)
}

func (b *acrossBridge) requiresBridgeContractApproval(sourceAssetID uint32) bool {
	return sourceAssetID != b.chainAssetID
}

// suggestedFeesRes is the response structure for the Across suggested-fees API.
type suggestedFeesRes struct {
	TotalRelayFee struct {
		Pct   string `json:"pct"`
		Total string `json:"total"`
	} `json:"totalRelayFee"`
	RelayerCapitalFee struct {
		Pct   string `json:"pct"`
		Total string `json:"total"`
	} `json:"relayerCapitalFee"`
	RelayerGasFee struct {
		Pct   string `json:"pct"`
		Total string `json:"total"`
	} `json:"relayerGasFee"`
	LpFee struct {
		Pct   string `json:"pct"`
		Total string `json:"total"`
	} `json:"lpFee"`
	Timestamp           string `json:"timestamp"`
	IsAmountTooLow      bool   `json:"isAmountTooLow"`
	QuoteBlock          string `json:"quoteBlock"`
	SpokePoolAddress    string `json:"spokePoolAddress"`
	ExpectedFillTimeSec string `json:"expectedFillTimeSec"`
	FillDeadline        string `json:"fillDeadline"`
	Limits              struct {
		MinDeposit                string `json:"minDeposit"`
		MaxDeposit                string `json:"maxDeposit"`
		MaxDepositInstant         string `json:"maxDepositInstant"`
		MaxDepositShortDelay      string `json:"maxDepositShortDelay"`
		RecommendedDepositInstant string `json:"recommendedDepositInstant"`
	} `json:"limits"`
}

func (b *acrossBridge) initiateBridge(txOpts *bind.TransactOpts, sourceAssetID, destAssetID uint32, amount *big.Int) (*types.Transaction, error) {
	supportedDestinations, err := acrossSupportedDestinations(txOpts.Context, sourceAssetID, b.net, b.log)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch available routes: %w", err)
	}
	if !slices.Contains(supportedDestinations, destAssetID) {
		return nil, fmt.Errorf("bridging from %s to %s is not supported", dex.BipIDSymbol(sourceAssetID), dex.BipIDSymbol(destAssetID))
	}

	sourceAsset := assetIDToAcrossAsset(b.net, sourceAssetID)
	if sourceAsset == nil {
		return nil, fmt.Errorf("unknown source asset %d", sourceAssetID)
	}

	destAsset := assetIDToAcrossAsset(b.net, destAssetID)
	if destAsset == nil {
		return nil, fmt.Errorf("unknown destination asset %d", destAssetID)
	}

	url := fmt.Sprintf("%s/api/suggested-fees?inputToken=%s&outputToken=%s&originChainId=%d&destinationChainId=%d&amount=%s",
		b.apiBaseURL, sourceAsset.address.Hex(), destAsset.address.Hex(), b.chainID, destAsset.chainID, amount.String())

	var res suggestedFeesRes
	ctx := txOpts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var acrossErr struct {
		Message string `json:"message"`
	}
	if err := dexnet.Get(ctx, url, &res, dexnet.WithErrorParsing(&acrossErr)); err != nil {
		return nil, fmt.Errorf("failed to fetch suggested fees: %w, %s", err, acrossErr.Message)
	}

	if res.IsAmountTooLow {
		return nil, fmt.Errorf("amount too low for bridging")
	}
	relayFeeTotal, ok := new(big.Int).SetString(res.TotalRelayFee.Total, 10)
	if !ok {
		return nil, fmt.Errorf("invalid relay fee total")
	}
	outputAmount := new(big.Int).Sub(amount, relayFeeTotal)
	quoteTimestamp, ok := new(big.Int).SetString(res.Timestamp, 10)
	if !ok {
		return nil, fmt.Errorf("invalid quote timestamp")
	}
	fillDeadline, ok := new(big.Int).SetString(res.FillDeadline, 10)
	if !ok {
		return nil, fmt.Errorf("invalid fill deadline")
	}
	exclusivityDeadline := uint32(0)
	exclusiveRelayer := common.Address{}
	message := []byte{}
	recipient := b.addr
	depositor := b.addr
	destChainIdBig := big.NewInt(int64(destAsset.chainID))
	if sourceAssetID == b.chainAssetID {
		txOpts.Value = amount
	}

	// TODO: check that the fee is not too high before initiating the bridge. Consider a fee limit config.
	// On the UI the expected fee can be displayed and the user can approve it before proceeding, but some
	// other mechanism will be needed for market making.

	return b.spokePool.DepositV3(txOpts, depositor, recipient, sourceAsset.address, destAsset.address,
		amount, outputAmount, destChainIdBig, exclusiveRelayer, uint32(quoteTimestamp.Uint64()),
		uint32(fillDeadline.Uint64()), exclusivityDeadline, message)
}

func (b *acrossBridge) getCompletionData(ctx context.Context, sourceAssetID uint32, bridgeTxID string) ([]byte, error) {
	hash := common.HexToHash(bridgeTxID)
	receipt, err := b.node.transactionReceipt(ctx, hash)
	if err != nil {
		return nil, err
	}
	for _, log := range receipt.Logs {
		if log.Address == b.spokePoolAddr {

			// Across is going through a v2 -> v3 upgrade where the log type will
			// be changed. Currently, on August 6, 2025, the v2 log type is still
			// being used.
			// https://docs.across.to/introduction/migration-guides/migration-from-v2-to-v3#breaking-change-deprecating-across-v2-events
			event, err := b.spokePool.ParseFundsDeposited(*log)
			if err == nil {
				originBytes := common.LeftPadBytes(big.NewInt(int64(b.chainID)).Bytes(), 32)
				depositIdBytes := common.LeftPadBytes(event.DepositId.Bytes(), 32)
				return append(originBytes, depositIdBytes...), nil
			}

			if event, err := b.spokePool.ParseV3FundsDeposited(*log); err == nil {
				originBytes := common.LeftPadBytes(big.NewInt(int64(b.chainID)).Bytes(), 32)
				depositIdBytes := common.LeftPadBytes(big.NewInt(int64(event.DepositId)).Bytes(), 32)
				return append(originBytes, depositIdBytes...), nil
			}
		}
	}

	return nil, fmt.Errorf("no V3FundsDeposited event found in transaction %s", bridgeTxID)
}

func (b *acrossBridge) completeBridge(*bind.TransactOpts, uint32, []byte) (*types.Transaction, error) {
	return nil, fmt.Errorf("no completion transaction is required for across protocol")
}

func (b *acrossBridge) initiateBridgeGas(uint32) uint64 {
	return 150_000
}

func (b *acrossBridge) completeBridgeGas(uint32) uint64 {
	return 0
}

func (b *acrossBridge) requiresCompletion(uint32) bool {
	return false
}

func (b *acrossBridge) verifyBridgeCompletion(ctx context.Context, data []byte) (bool, error) {
	if len(data) != 64 {
		return false, fmt.Errorf("invalid completion data length: expected 64, got %d", len(data))
	}
	if b.net != dex.Mainnet {
		// Across docs: For best results, we recommend using the deposit/status
		// endpoint on mainnet only. Our event indexing currently focuses on
		// mainnet, so testnet queries may return incomplete data.
		return true, nil
	}

	originChainId := new(big.Int).SetBytes(data[0:32]).Uint64()
	depositId := new(big.Int).SetBytes(data[32:64]).Uint64()
	url := fmt.Sprintf("%s/api/deposit/status?originChainId=%d&depositId=%d", b.apiBaseURL, originChainId, depositId)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var res struct {
		Status string `json:"status"`
	}
	if err := dexnet.Get(ctx, url, &res); err != nil {
		return false, fmt.Errorf("failed to fetch deposit status: %w", err)
	}

	return res.Status == "filled", nil
}

func (b *acrossBridge) supportedDestinations(sourceAssetID uint32) []uint32 {
	destinations, err := acrossSupportedDestinations(context.Background(), sourceAssetID, b.net, b.log)
	if err != nil {
		b.log.Errorf("Across bridge: failed to fetch supported destinations: %v", err)
		return nil
	}
	return destinations
}

func (b *acrossBridge) requiresFollowUpCompletion(uint32) bool {
	return false
}

func (b *acrossBridge) getFollowUpCompletionData(ctx context.Context, completionTxID string) (required bool, data []byte, err error) {
	panic("not implemented")
}

func (b *acrossBridge) completeFollowUpBridge(txOpts *bind.TransactOpts, data []byte) (tx *types.Transaction, err error) {
	return nil, fmt.Errorf("no follow-up completion is required for across protocol")
}

func (b *acrossBridge) followUpCompleteBridgeGas() uint64 {
	return 0
}

func (b *acrossBridge) bridgeLimits(sourceAssetID, destAssetID uint32) (*big.Int, *big.Int, bool, error) {
	sourceAsset := assetIDToAcrossAsset(b.net, sourceAssetID)
	if sourceAsset == nil {
		return nil, nil, false, fmt.Errorf("unknown source asset %d", sourceAssetID)
	}

	destAsset := assetIDToAcrossAsset(b.net, destAssetID)
	if destAsset == nil {
		return nil, nil, false, fmt.Errorf("unknown destination asset %d", destAssetID)
	}

	url := fmt.Sprintf("%s/api/limits?inputToken=%s&outputToken=%s&originChainId=%d&destinationChainId=%d",
		b.apiBaseURL, sourceAsset.address.Hex(), destAsset.address.Hex(), b.chainID, destAsset.chainID)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var res struct {
		MinDeposit string `json:"minDeposit"`
		MaxDeposit string `json:"maxDeposit"`
	}
	if err := dexnet.Get(ctx, url, &res); err != nil {
		return nil, nil, false, fmt.Errorf("failed to fetch bridge limits: %w", err)
	}

	minDeposit, ok := new(big.Int).SetString(res.MinDeposit, 10)
	if !ok {
		return nil, nil, false, fmt.Errorf("invalid min deposit value: %s", res.MinDeposit)
	}

	maxDeposit, ok := new(big.Int).SetString(res.MaxDeposit, 10)
	if !ok {
		return nil, nil, false, fmt.Errorf("invalid max deposit value: %s", res.MaxDeposit)
	}

	return minDeposit, maxDeposit, true, nil
}
