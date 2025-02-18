// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/server/asset"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

const (
	BipID = 60
)

var (
	_ asset.Driver      = (*Driver)(nil)
	_ asset.TokenBacker = (*ETHBackend)(nil)

	defaultProtocolVersion    = dexeth.ProtocolVersionV1Contracts
	protocolVersionsFilePath  = "evm-protocol-overrides.json"
	protocolVersionsOverrides = make(map[uint32]dexeth.ProtocolVersion)

	backendInfo = &asset.BackendInfo{
		SupportsDynamicTxFee: true,
	}

	refundRecordHash = [32]byte{
		0xaf, 0x96, 0x13, 0x76, 0x0f, 0x72, 0x63, 0x5f, 0xbd, 0xb4, 0x4a, 0x5a,
		0x0a, 0x63, 0xc3, 0x9f, 0x12, 0xaf, 0x30, 0xf9, 0x50, 0xa6, 0xee, 0x5c,
		0x97, 0x1b, 0xe1, 0x88, 0xe8, 0x9c, 0x40, 0x51,
	}
)

func init() {
	// Load any legacy/reverted protocol versions.
	if b, err := os.ReadFile(protocolVersionsFilePath); err == nil && len(b) > 0 {
		var symbolVers map[string]uint32
		if err := json.Unmarshal(b, &symbolVers); err != nil {
			panic(fmt.Sprintf("provided protocol override file at %q did not parse: %v", protocolVersionsFilePath, err))
		}
		for symbol, v := range symbolVers {
			assetID, found := dex.BipSymbolID(symbol)
			if !found {
				panic(fmt.Sprintf("asset %s specified in protocol override file is not known", symbol))
			}
			protocolVersionsOverrides[assetID] = dexeth.ProtocolVersion(v)
		}
	}

	asset.Register(BipID, &Driver{
		DriverBase: DriverBase{
			ProtocolVersion: ProtocolVersion(BipID),
			UI:              dexeth.UnitInfo,
			Nam:             "Ethereum",
		},
	})

	registerToken(usdcID, ProtocolVersion(usdcID))
	registerToken(usdtID, ProtocolVersion(usdtID))
	registerToken(maticID, ProtocolVersion(maticID))
}

// ProtocolVersion returns the default protocol version unless a reversion is
// specified in the file at protocolVersionsFilePath.
func ProtocolVersion(assetID uint32) dexeth.ProtocolVersion {
	v, found := protocolVersionsOverrides[assetID]
	if found {
		return v
	}
	return defaultProtocolVersion
}

type VersionedToken struct {
	*dexeth.Token
	ContractVersion uint32
}

var (
	registeredTokens = make(map[uint32]*VersionedToken)

	usdcID, _  = dex.BipSymbolID("usdc.eth")
	usdtID, _  = dex.BipSymbolID("usdt.eth")
	maticID, _ = dex.BipSymbolID("matic.eth")
)

func registerToken(assetID uint32, protocolVer dexeth.ProtocolVersion) {
	token, exists := dexeth.Tokens[assetID]
	if !exists {
		panic(fmt.Sprintf("no token constructor for asset ID %d", assetID))
	}
	drv := &TokenDriver{
		DriverBase: DriverBase{
			ProtocolVersion: protocolVer,
			UI:              token.UnitInfo,
			Nam:             token.Name,
		},
		Token: token.Token,
	}
	asset.RegisterToken(assetID, drv)
	registeredTokens[assetID] = &VersionedToken{
		Token:           token,
		ContractVersion: protocolVer.ContractVersion(),
	}
}

func networkToken(vToken *VersionedToken, net dex.Network) (netToken *dexeth.NetToken, contract *dexeth.SwapContract, err error) {
	netToken, found := vToken.NetTokens[net]
	if !found {
		return nil, nil, fmt.Errorf("no addresses for %s on %s", vToken.Name, net)
	}

	contract, found = netToken.SwapContracts[vToken.ContractVersion]
	if !found || (vToken.ContractVersion == 0 && contract.Address == (common.Address{})) {
		return nil, nil, fmt.Errorf("no version %d address for %s on %s", vToken.ContractVersion, vToken.Name, net)
	}
	return
}

type DriverBase struct {
	UI              dex.UnitInfo
	ProtocolVersion dexeth.ProtocolVersion
	Nam             string
}

// Version returns the Backend implementation's version number.
func (d *DriverBase) Version() uint32 {
	return uint32(d.ProtocolVersion)
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Ethereum. This must be a transaction hash.
func (d *DriverBase) DecodeCoinID(coinID []byte) (string, error) {
	txHash, err := dexeth.DecodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return txHash.String(), nil
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *DriverBase) UnitInfo() dex.UnitInfo {
	return d.UI
}

// Name is the asset's name.
func (d *DriverBase) Name() string {
	return d.Nam
}

// Driver implements asset.Driver.
type Driver struct {
	DriverBase
}

// Setup creates the ETH backend. Start the backend with its Run method.
func (d *Driver) Setup(cfg *asset.BackendConfig) (asset.Backend, error) {
	var chainID uint64
	switch cfg.Net {
	case dex.Mainnet:
		chainID = params.MainnetChainConfig.ChainID.Uint64()
	case dex.Testnet:
		chainID = params.SepoliaChainConfig.ChainID.Uint64()
	default:
		chainID = dexeth.SimnetChainID
	}

	for _, tkn := range registeredTokens {
		netToken, found := tkn.NetTokens[cfg.Net]
		if !found {
			if cfg.Net == dex.Mainnet {
				return nil, fmt.Errorf("no %s token for %s", tkn.Name, cfg.Net)
			}
			continue
		}
		if _, found = netToken.SwapContracts[tkn.ContractVersion]; !found {
			return nil, fmt.Errorf("no version %d swap contract adddress for %s on %s. "+
				"Do you need a version override in evm-protocol-overrides.json?",
				tkn.ContractVersion, tkn.Name, cfg.Net)
		}
	}

	return NewEVMBackend(cfg, chainID, dexeth.ContractAddresses, registeredTokens)
}

type TokenDriver struct {
	DriverBase
	Token *dex.Token
}

// TokenInfo returns details for a token asset.
func (d *TokenDriver) TokenInfo() *dex.Token {
	return d.Token
}

// ethFetcher represents a blockchain information fetcher. In practice, it is
// satisfied by rpcclient. For testing, it can be satisfied by a stub.
//
// TODO: At some point multiple contracts will need to be used, at least for
// transitory periods when updating the contract, and possibly a random
// contract setup, and so contract addresses may need to be an argument in some
// of these methods.
type ethFetcher interface {
	bestHeader(ctx context.Context) (*types.Header, error)
	blockNumber(ctx context.Context) (uint64, error)
	headerByHeight(ctx context.Context, height uint64) (*types.Header, error)
	connect(ctx context.Context) error
	suggestGasTipCap(ctx context.Context) (*big.Int, error)
	transaction(ctx context.Context, hash common.Hash) (tx *types.Transaction, isMempool bool, err error)
	transactionReceipt(ctx context.Context, txHash common.Hash) (*types.Receipt, error)
	// token- and asset-specific methods
	loadToken(ctx context.Context, assetID uint32, vToken *VersionedToken) error
	status(ctx context.Context, assetID uint32, token common.Address, locator []byte) (*dexeth.SwapStatus, error)
	vector(ctx context.Context, assetID uint32, locator []byte) (*dexeth.SwapVector, error)
	statusAndVector(ctx context.Context, assetID uint32, locator []byte) (*dexeth.SwapStatus, *dexeth.SwapVector, error)
	accountBalance(ctx context.Context, assetID uint32, addr common.Address) (*big.Int, error)
}

type baseBackend struct {
	// A connection-scoped Context is used to cancel active RPCs on
	// connection shutdown.
	ctx  context.Context
	net  dex.Network
	node ethFetcher

	baseChainID     uint32
	baseChainName   string
	versionedTokens map[uint32]*VersionedToken

	// bestHeight is the last best known chain tip height. bestHeight is set
	// in Connect before the poll loop is started, and only updated in the poll
	// loop thereafter. Do not use bestHeight outside of the poll loop unless
	// you change it to an atomic.
	bestHeight uint64

	// A logger will be provided by the DEX. All logging should use the provided
	// logger.
	baseLogger dex.Logger

	tokens map[uint32]*TokenBackend
}

// AssetBackend is an asset backend for Ethereum. It has methods for fetching output
// information and subscribing to block updates.
// AssetBackend implements asset.Backend, so provides exported methods for
// DEX-related blockchain info.
type AssetBackend struct {
	*baseBackend
	assetID   uint32
	tokenAddr common.Address
	log       dex.Logger
	atomize   func(*big.Int) uint64 // atomize takes floor. use for values, not fee rates
	gases     *dexeth.Gases

	// The backend provides block notification channels through the BlockChannel
	// method.
	blockChansMtx sync.RWMutex
	blockChans    map[chan *asset.BlockUpdate]struct{}

	contractAddr   common.Address // could be v0 or v1
	contractAddrV1 common.Address // required regardless
	contractVer    uint32
}

// ETHBackend implements some Ethereum-specific methods.
type ETHBackend struct {
	*AssetBackend
}

// TokenBackend implements some token-specific methods.
type TokenBackend struct {
	*AssetBackend
	*VersionedToken
}

// Check that Backend satisfies the Backend interface.
var _ asset.Backend = (*TokenBackend)(nil)
var _ asset.Backend = (*ETHBackend)(nil)

// Check that Backend satisfies the AccountBalancer interface.
var _ asset.AccountBalancer = (*TokenBackend)(nil)
var _ asset.AccountBalancer = (*ETHBackend)(nil)

// unconnectedETH returns a Backend without a node. The node should be set
// before use.
func unconnectedETH(bipID, contractVer uint32, contractAddr, contractAddrV1 common.Address, vTokens map[uint32]*VersionedToken, logger dex.Logger, net dex.Network) (*ETHBackend, error) {
	return &ETHBackend{&AssetBackend{
		baseBackend: &baseBackend{
			net:             net,
			baseLogger:      logger,
			tokens:          make(map[uint32]*TokenBackend),
			baseChainID:     bipID,
			baseChainName:   strings.ToUpper(dex.BipIDSymbol(bipID)),
			versionedTokens: vTokens,
		},
		log:            logger,
		contractAddr:   contractAddr,
		contractAddrV1: contractAddrV1,
		blockChans:     make(map[chan *asset.BlockUpdate]struct{}),
		assetID:        bipID,
		atomize:        dexeth.WeiToGwei,
		gases:          dexeth.VersionedGases[contractVer],
		contractVer:    contractVer,
	}}, nil
}

func parseEndpoints(cfg *asset.BackendConfig) ([]endpoint, error) {
	var endpoints []endpoint
	if cfg.RelayAddr != "" {
		endpoints = append(endpoints, endpoint{
			url:      "http://" + cfg.RelayAddr,
			priority: 10,
		})
	}
	file, err := os.Open(cfg.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) && len(endpoints) > 0 {
			return endpoints, nil
		}
		return nil, err
	}
	defer file.Close()

	assetName := strings.ToUpper(dex.BipIDSymbol(cfg.AssetID))

	endpointsMap := make(map[string]bool) // to avoid duplicates
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		ethCfgInstructions := "invalid %s config line: \"%s\". " +
			"Each line must contain URL and optionally a priority (between 0-65535) " +
			"separated by a comma. Example: \"https://www.infura.io/,2\""
		parts := strings.Split(line, ",")
		if len(parts) < 1 || len(parts) > 2 {
			return nil, fmt.Errorf(ethCfgInstructions, assetName, line)
		}

		url := strings.TrimSpace(parts[0])
		var priority uint16
		if len(parts) == 2 {
			priority64, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 16)
			if err != nil {
				return nil, fmt.Errorf(ethCfgInstructions, line)
			}
			priority = uint16(priority64)
		}
		if endpointsMap[url] {
			continue
		}
		endpointsMap[line] = true
		endpoints = append(endpoints, endpoint{
			url:      url,
			priority: priority,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading %s config file at %q. %v", assetName, cfg.ConfigPath, err)
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no endpoint found in the %s config file at %q", assetName, cfg.ConfigPath)
	}

	return endpoints, nil
}

// NewEVMBackend is the exported constructor by which the DEX will import the
// Backend.
func NewEVMBackend(
	cfg *asset.BackendConfig,
	chainID uint64,
	contractAddrs map[uint32]map[dex.Network]common.Address,
	vTokens map[uint32]*VersionedToken,
) (*ETHBackend, error) {

	endpoints, err := parseEndpoints(cfg)
	if err != nil {
		return nil, err
	}

	baseChainID, net, log := cfg.AssetID, cfg.Net, cfg.Logger
	assetName := strings.ToUpper(dex.BipIDSymbol(baseChainID))
	contractVer := ProtocolVersion(baseChainID).ContractVersion()

	netAddrs, found := contractAddrs[contractVer]
	if !found {
		return nil, fmt.Errorf("no contract address for %s version %d", assetName, contractVer)
	}
	contractAddr, found := netAddrs[net]
	if !found {
		return nil, fmt.Errorf("no contract address for %s version %d on %s", assetName, contractVer, net)
	}

	// v1 contract is required even if the base chain is using v0 for some
	// reason, because tokens might use v1.
	netAddrsV1, found := contractAddrs[1]
	if !found {
		return nil, fmt.Errorf("no v1 contract address for %s", assetName)
	}
	contractAddrV1, found := netAddrsV1[net]
	if !found {
		return nil, fmt.Errorf("no v1 contract address for %s on %s", assetName, net)
	}

	eth, err := unconnectedETH(baseChainID, contractVer, contractAddr, contractAddrV1, vTokens, log, net)
	if err != nil {
		return nil, err
	}

	eth.node = newRPCClient(baseChainID, chainID, net, endpoints, contractVer, contractAddr, contractAddrV1, log.SubLogger("RPC"))
	return eth, nil
}

// Connect connects to the node RPC server and initializes some variables.
func (eth *ETHBackend) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	eth.baseBackend.ctx = ctx

	// Create a separate context for the node so that it will only be cancelled
	// after the ETHBackend's run method has returned.
	nodeContext, cancelNodeContext := context.WithCancel(context.Background())
	if err := eth.node.connect(nodeContext); err != nil {
		cancelNodeContext()
		return nil, err
	}

	// Prime the best block hash and height.
	bn, err := eth.node.blockNumber(ctx)
	if err != nil {
		cancelNodeContext()
		return nil, fmt.Errorf("error getting best block header: %w", err)
	}
	eth.baseBackend.bestHeight = bn

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		eth.run(ctx)
		cancelNodeContext()
		wg.Done()
	}()
	return &wg, nil
}

// Connect for TokenBackend just waits for context cancellation and closes the
// WaitGroup.
func (eth *TokenBackend) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	if eth.baseBackend.ctx == nil || eth.baseBackend.ctx.Err() != nil {
		return nil, fmt.Errorf("parent asset not connected")
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		select {
		case <-ctx.Done():
		case <-eth.baseBackend.ctx.Done():
		}
	}()
	return &wg, nil
}

// TokenBackend creates an *AssetBackend for a token. Part of the
// asset.TokenBacker interface. Do not call TokenBackend concurrently for the
// same asset.
func (eth *ETHBackend) TokenBackend(assetID uint32, configPath string) (asset.Backend, error) {
	if _, found := eth.baseBackend.tokens[assetID]; found {
		return nil, fmt.Errorf("asset %d backend already loaded", assetID)
	}

	vToken, found := eth.versionedTokens[assetID]
	if !found {
		return nil, fmt.Errorf("no token for asset ID %d", assetID)
	}

	netToken, swapContract, err := networkToken(vToken, eth.net)
	if err != nil {
		return nil, err
	}

	contractAddr := swapContract.Address
	if vToken.ContractVersion == 1 {
		contractAddr = eth.contractAddrV1
	}

	if err := eth.node.loadToken(eth.ctx, assetID, vToken); err != nil {
		return nil, fmt.Errorf("error loading token for asset ID %d: %w", assetID, err)
	}
	be := &TokenBackend{
		AssetBackend: &AssetBackend{
			baseBackend:  eth.baseBackend,
			tokenAddr:    netToken.Address,
			log:          eth.baseLogger.SubLogger(strings.ToUpper(dex.BipIDSymbol(assetID))),
			assetID:      assetID,
			blockChans:   make(map[chan *asset.BlockUpdate]struct{}),
			contractAddr: contractAddr,
			atomize:      vToken.EVMToAtomic,
			gases:        &swapContract.Gas,
		},
		VersionedToken: vToken,
	}
	eth.baseBackend.tokens[assetID] = be
	return be, nil
}

// TxData fetches the raw transaction data.
func (eth *baseBackend) TxData(coinID []byte) ([]byte, error) {
	txHash, err := dexeth.DecodeCoinID(coinID)
	if err != nil {
		return nil, fmt.Errorf("coin ID decoding error: %v", err)
	}

	tx, _, err := eth.node.transaction(eth.ctx, txHash)
	if err != nil {
		return nil, fmt.Errorf("error retrieving transaction: %w", err)
	}
	if tx == nil { // Possible?
		return nil, fmt.Errorf("no transaction %s", txHash)
	}

	return tx.MarshalBinary()
}

// InitTxSize is an upper limit on the gas used for an initiation.
func (be *AssetBackend) InitTxSize() uint64 {
	return be.gases.Swap
}

// RedeemSize is the same as (dex.Asset).RedeemSize for the asset.
func (be *AssetBackend) RedeemSize() uint64 {
	return be.gases.Redeem
}

// FeeRate returns the current optimal fee rate in gwei / gas.
func (eth *baseBackend) FeeRate(ctx context.Context) (uint64, error) {
	hdr, err := eth.node.bestHeader(ctx)
	if err != nil {
		return 0, fmt.Errorf("error getting best header: %w", err)
	}

	if hdr.BaseFee == nil {
		return 0, fmt.Errorf("%v block header does not contain base fee", eth.baseChainName)
	}

	suggestedGasTipCap, err := eth.node.suggestGasTipCap(ctx)
	if err != nil {
		return 0, fmt.Errorf("error getting suggested gas tip cap: %w", err)
	}

	feeRate := new(big.Int).Add(
		suggestedGasTipCap,
		new(big.Int).Mul(hdr.BaseFee, big.NewInt(2)))

	feeRateGwei, err := dexeth.WeiToGweiSafe(feeRate)
	if err != nil {
		return 0, fmt.Errorf("failed to convert wei to gwei: %w", err)
	}

	return feeRateGwei, nil
}

// Info provides some general information about the backend.
func (*baseBackend) Info() *asset.BackendInfo {
	return backendInfo
}

// ValidateFeeRate checks that the transaction fees used to initiate the
// contract are sufficient. For most assets only the contract.FeeRate() cannot
// be less than reqFeeRate, but for Eth, the gasTipCap must also be checked.
func (eth *baseBackend) ValidateFeeRate(coin asset.Coin, reqFeeRate uint64) bool {
	sc, ok := coin.(*swapCoin)
	if !ok {
		eth.baseLogger.Error("%v contract coin type must be a swapCoin but got %T", eth.baseChainName, sc)
		return false
	}

	// Legacy transactions are also supported. In a legacy transaction, the
	// gas tip cap will be equal to the gas price.
	if dexeth.WeiToGwei(sc.gasTipCap) < dexeth.MinGasTipCap {
		sc.backend.log.Errorf("Transaction %s tip cap %d < %d", dexeth.WeiToGwei(sc.gasTipCap), dexeth.MinGasTipCap)
		return false
	}

	if sc.gasFeeCap.Cmp(dexeth.GweiToWei(reqFeeRate)) < 0 {
		sc.backend.log.Errorf("Transaction %s gas fee cap too low. %s wei / gas < %s gwei / gas", sc.gasFeeCap, reqFeeRate)
		return false
	}

	return true
}

// BlockChannel creates and returns a new channel on which to receive block
// updates. If the returned channel is ever blocking, there will be no error
// logged from the eth package. Part of the asset.Backend interface.
func (be *AssetBackend) BlockChannel(size int) <-chan *asset.BlockUpdate {
	c := make(chan *asset.BlockUpdate, size)
	be.blockChansMtx.Lock()
	defer be.blockChansMtx.Unlock()
	be.blockChans[c] = struct{}{}
	return c
}

// sendBlockUpdate sends the BlockUpdate to all subscribers.
func (be *AssetBackend) sendBlockUpdate(u *asset.BlockUpdate) {
	be.blockChansMtx.RLock()
	defer be.blockChansMtx.RUnlock()
	for c := range be.blockChans {
		select {
		case c <- u:
		default:
			be.log.Error("failed to send block update on blocking channel")
		}
	}
}

// ValidateContract ensures that contractData encodes both the expected contract
// version and a secret hash.
func (eth *ETHBackend) ValidateContract(contractData []byte) error {
	_, _, err := dexeth.DecodeContractData(contractData)
	if err != nil { // ensures secretHash is proper length
		return err
	}
	return nil
}

// ValidateContract ensures that contractData encodes both the expected swap
// contract version and a secret hash.
func (eth *TokenBackend) ValidateContract(contractData []byte) error {
	contractVer, _, err := dexeth.DecodeContractData(contractData)
	if err != nil { // ensures secretHash is proper length
		return err
	}
	_, _, err = networkToken(eth.VersionedToken, eth.net)
	if err != nil {
		return fmt.Errorf("error locating token: %v", err)
	}
	if contractVer != eth.VersionedToken.ContractVersion {
		return fmt.Errorf("incorrect token swap contract version %d, wanted %d", contractVer, eth.VersionedToken.ContractVersion)
	}

	return nil
}

// Contract is part of the asset.Backend interface. The contractData bytes
// encodes both the contract version targeted and the secret hash.
func (be *AssetBackend) Contract(coinID, contractData []byte) (*asset.Contract, error) {
	// newSwapCoin validates the contractData, extracting version, secret hash,
	// counterparty address, and locktime. The supported version is enforced.
	sc, err := be.newSwapCoin(coinID, contractData)
	if err != nil {
		return nil, fmt.Errorf("unable to create coiner: %w", err)
	}

	// Confirmations performs some extra swap status checks if the the tx is
	// mined. For init coins, this uses the contract account's state (if it is
	// mined) to verify the value, counterparty, and lock time.
	_, err = sc.Confirmations(be.ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get confirmations: %v", err)
	}

	return &asset.Contract{
		Coin:         sc,
		SwapAddress:  sc.vector.To.String(),
		ContractData: contractData,
		SecretHash:   sc.vector.SecretHash[:],
		TxData:       sc.serializedTx,
		LockTime:     time.Unix(int64(sc.vector.LockTime), 0),
	}, nil
}

// ValidateSecret checks that the secret satisfies the secret hash.
func (eth *AssetBackend) ValidateSecret(secret, contractData []byte) bool {
	contractVer, locator, err := dexeth.DecodeContractData(contractData)
	if err != nil {
		eth.log.Errorf("Error decoding contract data for validation: %v", err)
		return false
	}
	var secretHash [32]byte
	switch contractVer {
	case 0:
		copy(secretHash[:], locator)
	case 1:
		v, err := dexeth.ParseV1Locator(locator)
		if err != nil {
			eth.log.Errorf("ValidateSecret locator parsing error: %v", err)
			return false
		}
		secretHash = v.SecretHash
	default:
		eth.log.Errorf("ValidateSecret received unknown contract version: %d", contractVer)
		return false
	}

	sh := sha256.Sum256(secret)
	return bytes.Equal(sh[:], secretHash[:])
}

// Synced is true if the blockchain is ready for action.
func (eth *baseBackend) Synced() (bool, error) {
	bh, err := eth.node.bestHeader(eth.ctx)
	if err != nil {
		return false, err
	}
	// Time in the header is in seconds.
	nowInSecs := time.Now().Unix()
	timeDiff := nowInSecs - int64(bh.Time)
	return timeDiff < dexeth.MaxBlockInterval, nil
}

// Redemption returns a coin that represents a contract redemption. redeemCoinID
// should be the transaction that sent a redemption, while contractCoinID is the
// swap contract this redemption redeems.
func (be *AssetBackend) Redemption(redeemCoinID, _, contractData []byte) (asset.Coin, error) {
	// newRedeemCoin uses the contract account's state to validate the
	// contractData, extracting version, secret, and secret hash. The supported
	// version is enforced.
	rc, err := be.newRedeemCoin(redeemCoinID, contractData)
	if err != nil {
		return nil, fmt.Errorf("unable to create coiner: %w", err)
	}

	// Confirmations performs some extra swap status checks if the the tx
	// is mined. For redeem coins, this is just a swap state check.
	_, err = rc.Confirmations(be.ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get confirmations: %v", err)
	}
	return rc, nil
}

// ValidateCoinID attempts to decode the coinID.
func (eth *baseBackend) ValidateCoinID(coinID []byte) (string, error) {
	txHash, err := dexeth.DecodeCoinID(coinID)
	if err != nil {
		return "<invalid>", err
	}
	return txHash.String(), nil
}

// CheckSwapAddress checks that the given address is parseable.
func (eth *baseBackend) CheckSwapAddress(addr string) bool {
	return common.IsHexAddress(addr)
}

// AccountBalance retrieves the current account balance, including the effects
// of known unmined transactions.
func (be *AssetBackend) AccountBalance(addrStr string) (uint64, error) {
	bigBal, err := be.node.accountBalance(be.ctx, be.assetID, common.HexToAddress(addrStr))
	if err != nil {
		return 0, fmt.Errorf("accountBalance error: %w", err)
	}
	return be.atomize(bigBal), nil
}

// ValidateSignature checks that the pubkey is correct for the address and
// that the signature shows ownership of the associated private key.
func (eth *baseBackend) ValidateSignature(addr string, pubkey, msg, sig []byte) error {
	const sigLen = 64
	if len(sig) != sigLen {
		return fmt.Errorf("expected sig length of %d bytes but got %d", sigLen, len(sig))
	}
	ethPK, err := crypto.UnmarshalPubkey(pubkey)
	if err != nil {
		return fmt.Errorf("unable to unmarshal pubkey: %v", err)
	}
	// Ensure the pubkey matches the funding coin's account address.
	// Internally, this will take the last twenty bytes of crypto.Keccak256
	// of all but the first byte of the pubkey bytes and wrap it as a
	// common.Address.
	pubkeyAddr := crypto.PubkeyToAddress(*ethPK)
	if addr != pubkeyAddr.String() {
		return errors.New("pubkey does not correspond to address")
	}
	r := new(secp256k1.ModNScalar)
	if overflow := r.SetByteSlice(sig[0:32]); overflow {
		return errors.New("invalid signature: r >= group order")
	}
	s := new(secp256k1.ModNScalar)
	if overflow := s.SetByteSlice(sig[32:64]); overflow {
		return errors.New("invalid signature: s >= group order")
	}
	ecdsaSig := ecdsa.NewSignature(r, s)

	pk, err := secp256k1.ParsePubKey(pubkey)
	if err != nil {
		return err
	}
	// Verify the signature.
	if !ecdsaSig.Verify(crypto.Keccak256(msg), pk) {
		return errors.New("cannot verify signature")
	}
	return nil
}

// poll pulls the best hash from an eth node and compares that to a stored
// hash. If the same does nothing. If different, updates the stored hash and
// notifies listeners on block chans.
func (eth *ETHBackend) poll(ctx context.Context) {
	send := func(err error) {
		if err != nil {
			eth.log.Error(err)
		}
		u := &asset.BlockUpdate{
			Err: err,
		}

		eth.sendBlockUpdate(u)

		for _, be := range eth.tokens {
			be.sendBlockUpdate(u)
		}
	}
	bn, err := eth.node.blockNumber(ctx)
	if err != nil {
		send(fmt.Errorf("error getting best block header: %w", err))
		return
	}
	if bn == eth.bestHeight {
		// Same height, nothing to do.
		return
	}
	eth.log.Debugf("Tip change from %d to %d.", eth.bestHeight, bn)
	eth.bestHeight = bn
	send(nil)
}

// run processes the queue and monitors the application context.
func (eth *ETHBackend) run(ctx context.Context) {
	// Non-loopback providers are metered at 10 seconds internally to rpcclient,
	// but loopback addresses allow more frequent checks.
	blockPoll := time.NewTicker(time.Second)
	defer blockPoll.Stop()

	for {
		select {
		case <-blockPoll.C:
			eth.poll(ctx)
		case <-ctx.Done():
			return
		}
	}
}
