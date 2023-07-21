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

type VersionedToken struct {
	*dexeth.Token
	Ver uint32
}

var registeredTokens = make(map[uint32]*VersionedToken)

func registerToken(assetID uint32, ver uint32) {
	token, exists := dexeth.Tokens[assetID]
	if !exists {
		panic(fmt.Sprintf("no token constructor for asset ID %d", assetID))
	}
	asset.RegisterToken(assetID, &TokenDriver{
		DriverBase: DriverBase{
			Ver: ver,
			UI:  token.UnitInfo,
		},
		Token: token.Token,
	})
	registeredTokens[assetID] = &VersionedToken{
		Token: token,
		Ver:   ver,
	}
}

func init() {
	asset.Register(BipID, &Driver{
		DriverBase: DriverBase{
			Ver: version,
			UI:  dexeth.UnitInfo,
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

func networkToken(vToken *VersionedToken, net dex.Network) (netToken *dexeth.NetToken, contract *dexeth.SwapContract, err error) {
	netToken, found := vToken.NetTokens[net]
	if !found {
		return nil, nil, fmt.Errorf("no addresses for %s on %s", vToken.Name, net)
	}
	contract, found = netToken.SwapContracts[vToken.Ver]
	if !found || contract.Address == (common.Address{}) {
		return nil, nil, fmt.Errorf("no version %d address for %s on %s", vToken.Ver, vToken.Name, net)
	}
	return
}

type DriverBase struct {
	UI  dex.UnitInfo
	Ver uint32
}

// Version returns the Backend implementation's version number.
func (d *DriverBase) Version() uint32 {
	return d.Ver
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

// Driver implements asset.Driver.
type Driver struct {
	DriverBase
}

// Setup creates the ETH backend. Start the backend with its Run method.
func (d *Driver) Setup(configPath string, logger dex.Logger, network dex.Network) (asset.Backend, error) {
	return NewEVMBackend(BipID, configPath, logger, dexeth.ContractAddresses, registeredTokens, network)
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
	// token- and asset-specific methods
	loadToken(ctx context.Context, assetID uint32, vToken *VersionedToken) error
	swap(ctx context.Context, assetID uint32, secretHash [32]byte) (*dexeth.SwapState, error)
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
func unconnectedETH(bipID uint32, contractAddr common.Address, vTokens map[uint32]*VersionedToken, logger dex.Logger, net dex.Network) (*ETHBackend, error) {
	// TODO: At some point multiple contracts will need to be used, at
	// least for transitory periods when updating the contract, and
	// possibly a random contract setup, and so this section will need to
	// change to support multiple contracts.
	return &ETHBackend{&AssetBackend{
		baseBackend: &baseBackend{
			net:             net,
			baseLogger:      logger,
			tokens:          make(map[uint32]*TokenBackend),
			baseChainID:     bipID,
			baseChainName:   strings.ToUpper(dex.BipIDSymbol(bipID)),
			versionedTokens: vTokens,
		},
		log:          logger,
		contractAddr: contractAddr,
		blockChans:   make(map[chan *asset.BlockUpdate]struct{}),
		initTxSize:   uint32(dexeth.InitGas(1, ethContractVersion)),
		redeemSize:   dexeth.RedeemGas(1, ethContractVersion),
		assetID:      bipID,
		atomize:      dexeth.WeiToGwei,
	}}, nil
}

// NewEVMBackend is the exported constructor by which the DEX will import the
// Backend.
func NewEVMBackend(
	baseChainID uint32,
	configPath string,
	log dex.Logger,
	contractAddrs map[uint32]map[dex.Network]common.Address,
	vTokens map[uint32]*VersionedToken,
	net dex.Network,
) (*ETHBackend, error) {

	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	assetName := strings.ToUpper(dex.BipIDSymbol(baseChainID))

	var endpoints []endpoint
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
		return nil, fmt.Errorf("error reading %s config file at %q. %v", assetName, configPath, err)
	}
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no endpoint found in the %s config file at %q", assetName, configPath)
	}
	log.Debugf("Parsed %d endpoints from the %v config file", assetName, len(endpoints))

	netAddrs, found := contractAddrs[ethContractVersion]
	if !found {
		return nil, fmt.Errorf("no contract address for %s version %d", assetName, ethContractVersion)
	}
	contractAddr, found := netAddrs[net]
	if !found {
		return nil, fmt.Errorf("no contract address for %s version %d on %s", assetName, ethContractVersion, net)
	}

	eth, err := unconnectedETH(baseChainID, contractAddr, vTokens, log, net)
	if err != nil {
		return nil, err
	}

	eth.node = newRPCClient(baseChainID, net, endpoints, contractAddr, log.SubLogger("RPC"))
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

	_, swapContract, err := networkToken(vToken, eth.net)
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

	if err := eth.node.loadToken(eth.ctx, assetID, vToken); err != nil {
		return nil, fmt.Errorf("error loading token for asset ID %d: %w", assetID, err)
	}
	be := &TokenBackend{
		AssetBackend: &AssetBackend{
			baseBackend:  eth.baseBackend,
			log:          eth.baseLogger.SubLogger(strings.ToUpper(dex.BipIDSymbol(assetID))),
			assetID:      assetID,
			blockChans:   make(map[chan *asset.BlockUpdate]struct{}),
			initTxSize:   uint32(gases.Swap),
			redeemSize:   gases.Redeem,
			contractAddr: swapContract.Address,
			atomize:      vToken.EVMToAtomic,
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
		return 0, fmt.Errorf("%v block header does not contain base fee", eth.baseChainName)
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
		eth.baseLogger.Error("%v contract coin type must be a swapCoin but got %T", eth.baseChainName, sc)
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
	_, _, err = networkToken(eth.VersionedToken, eth.net)
	if err != nil {
		return fmt.Errorf("error locating token: %v", err)
	}
	if ver != eth.VersionedToken.Ver {
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
	eth.baseLogger.Infof("Starting %v block polling with interval of %v",
		eth.baseChainName, blockPollInterval)
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
