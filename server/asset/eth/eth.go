// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package eth

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/server/asset"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type registeredToken struct {
	*dexeth.Token
	drv *TokenDriver
	ver uint32
}

var registeredTokens = make(map[uint32]*registeredToken)

func networkToken(assetID uint32, net dex.Network) (token *registeredToken, netToken *dexeth.NetToken, contract *dexeth.SwapContract, err error) {
	token, found := registeredTokens[assetID]
	if !found {
		return nil, nil, nil, fmt.Errorf("no token for asset ID %d", assetID)
	}
	netToken, found = token.NetTokens[net]
	if !found {
		return nil, nil, nil, fmt.Errorf("no addresses for %s on %s", token.Name, net)
	}
	contract, found = netToken.SwapContracts[token.ver]
	if !found || contract.Address == (common.Address{}) {
		return nil, nil, nil, fmt.Errorf("no version %d address for %s on %s", token.ver, token.Name, net)
	}
	return
}

func registerToken(assetID uint32, ver uint32) {
	token, exists := dexeth.Tokens[assetID]
	if !exists {
		panic(fmt.Sprintf("no token constructor for asset ID %d", assetID))
	}
	drv := &TokenDriver{
		driverBase: driverBase{
			version:  ver,
			unitInfo: token.UnitInfo,
		},
		token: token.Token,
	}
	asset.RegisterToken(assetID, drv)
	registeredTokens[assetID] = &registeredToken{
		Token: token,
		drv:   drv,
		ver:   ver,
	}
}

func init() {
	asset.Register(BipID, &Driver{
		driverBase: driverBase{
			version:  version,
			unitInfo: dexeth.UnitInfo,
		},
	})

	registerToken(testTokenID, 0)
	registerToken(usdcID, 0)

	if blockPollIntervalStr != "" {
		blockPollInterval, _ = time.ParseDuration(blockPollIntervalStr)
		if blockPollInterval < time.Second {
			panic(fmt.Sprintf("invalid value for blockPollIntervalStr: %q", blockPollIntervalStr))
		}
	}
}

const (
	BipID              = 60
	ethContractVersion = 0
	version            = 0
)

var (
	_ asset.Driver      = (*Driver)(nil)
	_ asset.TokenBacker = (*ETHBackend)(nil)

	backendInfo = &asset.BackendInfo{
		SupportsDynamicTxFee: true,
	}

	testTokenID, _ = dex.BipSymbolID("dextt.eth")
	usdcID, _      = dex.BipSymbolID("usdc.eth")

	// blockPollInterval is the delay between calls to bestBlockHash to check
	// for new blocks. Modify at compile time via blockPollIntervalStr:
	// go build -tags lgpl -ldflags "-X 'decred.org/dcrdex/server/asset/eth.blockPollIntervalStr=10s'"
	blockPollInterval    = time.Second
	blockPollIntervalStr string
)

type driverBase struct {
	unitInfo dex.UnitInfo
	version  uint32
}

// Version returns the Backend implementation's version number.
func (d *driverBase) Version() uint32 {
	return d.version
}

// DecodeCoinID creates a human-readable representation of a coin ID for
// Ethereum. This must be a transaction hash.
func (d *driverBase) DecodeCoinID(coinID []byte) (string, error) {
	txHash, err := dexeth.DecodeCoinID(coinID)
	if err != nil {
		return "", err
	}
	return txHash.String(), nil
}

// UnitInfo returns the dex.UnitInfo for the asset.
func (d *driverBase) UnitInfo() dex.UnitInfo {
	return d.unitInfo
}

// Driver implements asset.Driver.
type Driver struct {
	driverBase
}

// Setup creates the ETH backend. Start the backend with its Run method.
func (d *Driver) Setup(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	return NewBackend(configPath, logger, network)
}

type TokenDriver struct {
	driverBase
	token *dex.Token
}

// TokenInfo returns details for a token asset.
func (d *TokenDriver) TokenInfo() *dex.Token {
	return d.token
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
	// token- and asset-specific methods
	loadToken(ctx context.Context, assetID uint32) error
	swap(ctx context.Context, assetID uint32, secretHash [32]byte) (*dexeth.SwapState, error)
	accountBalance(ctx context.Context, assetID uint32, addr common.Address) (*big.Int, error)
}

type baseBackend struct {
	// A connection-scoped Context is used to cancel active RPCs on
	// connection shutdown.
	ctx  context.Context
	net  dex.Network
	node ethFetcher

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
	assetID uint32
	log     dex.Logger
	atomize func(*big.Int) uint64

	// The backend provides block notification channels through the BlockChannel
	// method.
	blockChansMtx sync.RWMutex
	blockChans    map[chan *asset.BlockUpdate]struct{}

	// initTxSize is the gas used for an initiation transaction with one swap.
	initTxSize uint32
	redeemSize uint64

	contractAddr common.Address
}

// ETHBackend implements some Ethereum-specific methods.
type ETHBackend struct {
	*AssetBackend
}

// TokenBackend implements some token-specific methods.
type TokenBackend struct {
	*AssetBackend
}

// Check that Backend satisfies the Backend interface.
var _ asset.Backend = (*TokenBackend)(nil)
var _ asset.Backend = (*ETHBackend)(nil)

// Check that Backend satisfies the AccountBalancer interface.
var _ asset.AccountBalancer = (*TokenBackend)(nil)
var _ asset.AccountBalancer = (*ETHBackend)(nil)

// unconnectedETH returns a Backend without a node. The node should be set
// before use.
func unconnectedETH(logger dex.Logger, net dex.Network) (*ETHBackend, error) {
	// TODO: At some point multiple contracts will need to be used, at
	// least for transitory periods when updating the contract, and
	// possibly a random contract setup, and so this section will need to
	// change to support multiple contracts.
	contractAddr, exists := dexeth.ContractAddresses[0][net]
	if !exists || contractAddr == (common.Address{}) {
		return nil, fmt.Errorf("no eth contract for version 0, net %s", net)
	}
	return &ETHBackend{&AssetBackend{
		baseBackend: &baseBackend{
			net:        net,
			baseLogger: logger,
			tokens:     make(map[uint32]*TokenBackend),
		},
		log:          logger,
		contractAddr: contractAddr,
		blockChans:   make(map[chan *asset.BlockUpdate]struct{}),
		initTxSize:   uint32(dexeth.InitGas(1, ethContractVersion)),
		redeemSize:   dexeth.RedeemGas(1, ethContractVersion),
		assetID:      BipID,
		atomize:      dexeth.WeiToGwei,
	}}, nil
}

// NewBackend is the exported constructor by which the DEX will import the
// Backend.
func NewBackend(configPath string, log dex.Logger, net dex.Network) (*ETHBackend, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var endpoints []endpoint
	endpointsMap := make(map[string]bool) // to avoid duplicates
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		ethCfgInstructions := "invalid eth config line: \"%s\". " +
			"Each line must contain URL and optionally a priority (between 0-65535) " +
			"separated by a comma. Example: \"https://www.infura.io/,2\""
		parts := strings.Split(line, ",")
		if len(parts) < 1 || len(parts) > 2 {
			return nil, fmt.Errorf(ethCfgInstructions, line)
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
		return nil, fmt.Errorf("error reading eth config file at %q. %v", configPath, err)
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no endpoint found in the eth config file at %q", configPath)
	}
	log.Debugf("Parsed %d endpoints from the ETH config file", len(endpoints))

	eth, err := unconnectedETH(log, net)
	if err != nil {
		return nil, err
	}

	netAddrs, found := dexeth.ContractAddresses[ethContractVersion]
	if !found {
		return nil, fmt.Errorf("no contract address for eth version %d", ethContractVersion)
	}
	ethContractAddr, found := netAddrs[eth.net]
	if !found {
		return nil, fmt.Errorf("no contract address for eth version %d on %s", ethContractVersion, eth.net)
	}

	eth.node = newRPCClient(net, endpoints, ethContractAddr, log.SubLogger("RPC"))
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

	token, _, swapContract, err := networkToken(assetID, eth.net)
	if err != nil {
		return nil, err
	}

	gases := new(configuredTokenGases)
	if configPath != "" {
		if err := config.ParseInto(configPath, gases); err != nil {
			return nil, fmt.Errorf("error parsing fee overrides for token %d: %v", assetID, err)
		}
	}

	if gases.Swap == 0 {
		gases.Swap = swapContract.Gas.Swap
	}
	if gases.Redeem == 0 {
		gases.Redeem = swapContract.Gas.Redeem
	}

	if err := eth.node.loadToken(eth.ctx, assetID); err != nil {
		return nil, fmt.Errorf("error loading token for asset ID %d: %w", assetID, err)
	}
	be := &TokenBackend{&AssetBackend{
		baseBackend:  eth.baseBackend,
		log:          eth.baseLogger.SubLogger(strings.ToUpper(dex.BipIDSymbol(assetID))),
		assetID:      assetID,
		blockChans:   make(map[chan *asset.BlockUpdate]struct{}),
		initTxSize:   uint32(gases.Swap),
		redeemSize:   gases.Redeem,
		contractAddr: swapContract.Address,
		atomize:      token.EVMToAtomic,
	}}
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
func (be *AssetBackend) InitTxSize() uint32 {
	return be.initTxSize
}

// InitTxSizeBase is the same as (dex.Asset).SwapSize for asset.
func (be *AssetBackend) InitTxSizeBase() uint32 {
	return be.initTxSize
}

// RedeemSize is the same as (dex.Asset).RedeemSize for the asset.
func (be *AssetBackend) RedeemSize() uint64 {
	return be.redeemSize
}

// FeeRate returns the current optimal fee rate in gwei / gas.
func (eth *baseBackend) FeeRate(ctx context.Context) (uint64, error) {
	hdr, err := eth.node.bestHeader(ctx)
	if err != nil {
		return 0, fmt.Errorf("error getting best header: %w", err)
	}

	if hdr.BaseFee == nil {
		return 0, errors.New("eth block header does not contain base fee")
	}

	suggestedGasTipCap, err := eth.node.suggestGasTipCap(ctx)
	if err != nil {
		return 0, fmt.Errorf("error getting suggested gas tip cap: %w", err)
	}

	feeRate := new(big.Int).Add(
		suggestedGasTipCap,
		new(big.Int).Mul(hdr.BaseFee, big.NewInt(2)))

	feeRateGwei, err := dexeth.WeiToGweiUint64(feeRate)
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
func (eth *baseBackend) ValidateFeeRate(contract *asset.Contract, reqFeeRate uint64) bool {
	coin := contract.Coin
	sc, ok := coin.(*swapCoin)
	if !ok {
		eth.baseLogger.Error("eth contract coin type must be a swapCoin but got %T", sc)
		return false
	}

	// Legacy transactions are also supported. In a legacy transaction, the
	// gas tip cap will be equal to the gas price.
	if sc.gasTipCap < dexeth.MinGasTipCap {
		return false
	}

	return contract.FeeRate() >= reqFeeRate
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
	ver, _, err := dexeth.DecodeContractData(contractData)
	if err != nil { // ensures secretHash is proper length
		return err
	}

	if ver != version {
		return fmt.Errorf("incorrect swap contract version %d, wanted %d", ver, version)
	}
	return nil
}

// ValidateContract ensures that contractData encodes both the expected swap
// contract version and a secret hash.
func (eth *TokenBackend) ValidateContract(contractData []byte) error {
	ver, _, err := dexeth.DecodeContractData(contractData)
	if err != nil { // ensures secretHash is proper length
		return err
	}

	token, _, _, err := networkToken(eth.assetID, eth.net)
	if err != nil {
		return fmt.Errorf("error locating token: %v", err)
	}
	if ver != token.ver {
		return fmt.Errorf("incorrect token swap contract version %d, wanted %d", ver, version)
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
		SwapAddress:  sc.init.Participant.String(),
		ContractData: contractData,
		SecretHash:   sc.secretHash[:],
		TxData:       sc.serializedTx,
		LockTime:     sc.init.LockTime,
	}, nil
}

// ValidateSecret checks that the secret satisfies the secret hash.
func (eth *baseBackend) ValidateSecret(secret, contractData []byte) bool {
	_, secretHash, err := dexeth.DecodeContractData(contractData)
	if err != nil {
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
	eth.baseLogger.Infof("Starting ETH block polling with interval of %v",
		blockPollInterval)
	blockPoll := time.NewTicker(blockPollInterval)
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
