package eth

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/dexnet"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/networks/erc20"
	"decred.org/dcrdex/dex/networks/erc20/cctp"
)

// bridge is the interface that must be implemented by a bridge.
type bridge interface {
	// bridgeContractAddr is the address that must be approved to spend tokens
	// in order to bridge.
	bridgeContractAddr() common.Address

	// bridgeContractAllowance returns the amount of tokens that have been
	// approved to be spent by the bridge contract.
	bridgeContractAllowance(ctx context.Context) (*big.Int, error)

	// approveBridgeContract approves the bridge contract to spend the given
	// amount of tokens.
	approveBridgeContract(txOpts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error)

	// requiresBridgeContractApproval returns true if the bridge contract must
	// be approved to spend tokens in order to bridge.
	requiresBridgeContractApproval() bool

	// initiateBridge burns or locks the asset in order to bridge it to the destination.
	// requiresCompletion is true if a transaction must be executed on the destination
	// chain to mint the asset.
	initiateBridge(txOpts *bind.TransactOpts, destAssetID uint32, amount *big.Int) (tx *types.Transaction, err error)

	// getCompletionData retrieves the data required by the destination chain
	// to complete the bridge.
	getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error)

	// completeBridge executes a transaction on the destination chain to complete
	// the bridge.
	completeBridge(txOpts *bind.TransactOpts, mintInfoB []byte) (tx *types.Transaction, err error)

	// initiateBridgeGas returns the gas cost of the bridge transaction.
	initiateBridgeGas() uint64

	// completeBridgeGas returns the gas cost of the mint transaction.
	completeBridgeGas() uint64

	// requiresCompletion is true if a transaction must be executed on the destination
	// chain to mint the asset. This is called on the destination chain. If this
	// returns false, verifyBridgeCompletion should be called.
	requiresCompletion() bool

	// verifyBridgeCompletion verifies that the bridge was completed successfully.
	// This is required for bridges that do not require a completion transaction.
	verifyBridgeCompletion(ctx context.Context, data []byte) (bool, error)
}

var (
	usdcEthID, _     = dex.BipSymbolID("usdc.eth")
	usdcPolygonID, _ = dex.BipSymbolID("usdc.polygon")
)

type usdcBridgeInfo struct {
	tokenMessengerAddr     common.Address
	messageTransmitterAddr common.Address
	domainID               uint32
}

var usdcBridgeInfos = map[uint32]map[dex.Network]*usdcBridgeInfo{
	usdcEthID: {
		dex.Mainnet: {
			tokenMessengerAddr:     common.HexToAddress("0xbd3fa81b58ba92a82136038b25adec7066af3155"),
			messageTransmitterAddr: common.HexToAddress("0x0a992d191deec32afe36203ad87d7d289a738f81"),
			domainID:               0,
		},
		dex.Testnet: {
			tokenMessengerAddr:     common.HexToAddress("0x9f3B8679c73C2Fef8b59B4f3444d4e156fb70AA5"),
			messageTransmitterAddr: common.HexToAddress("0x7865fAfC2db2093669d92c0F33AeEF291086BEFD"),
			domainID:               0,
		},
	},
	usdcPolygonID: {
		dex.Mainnet: {
			tokenMessengerAddr:     common.HexToAddress("0x9daF8c91AEFAE50b9c0E69629D3F6Ca40cA3B3FE"),
			messageTransmitterAddr: common.HexToAddress("0xF3be9355363857F3e001be68856A2f96b4C39Ba9"),
			domainID:               7,
		},
		dex.Testnet: {
			tokenMessengerAddr:     common.HexToAddress("0x9f3B8679c73C2Fef8b59B4f3444d4e156fb70AA5"),
			messageTransmitterAddr: common.HexToAddress("0x7865fAfC2db2093669d92c0F33AeEF291086BEFD"),
			domainID:               7,
		},
	},
}

var usdcBridgeAttestationUrl = map[dex.Network]string{
	dex.Mainnet: "https://iris-api.circle.com/attestations/",
	dex.Testnet: "https://iris-api-sandbox.circle.com/attestations/",
}

// getUsdcBridgeInfo returns the contract addresses for the USDC bridge on
// the specified asset and network.
func getUsdcBridgeInfo(assetID uint32, net dex.Network) (*usdcBridgeInfo, error) {
	assetBridgeInfo, found := usdcBridgeInfos[assetID]
	if !found {
		return nil, fmt.Errorf("usdc bridge info not found for assetID %d and network %s", assetID, net)
	}

	bridgeInfo, found := assetBridgeInfo[net]
	if !found {
		return nil, fmt.Errorf("usdc bridge info not found for assetID %d and network %s", assetID, net)
	}

	return bridgeInfo, nil
}

// isUSDCBridgeSupported returns true if the USDC bridge is supported for the
// specified asset and network.
func isUSDCBridgeSupported(assetID uint32, net dex.Network) bool {
	_, err := getUsdcBridgeInfo(assetID, net)
	return err == nil
}

// usdcBridge implements Circle's CCTP protocol to allow bridging native usdc
// between chains.
//
// https://developers.circle.com/stablecoins/docs/cctp-getting-started
//
// The bridge works using the following steps:
//
//  1. Call the "TokenMessenger" contract to burn the USDC on the source chain.
//     The "TokenMessenger" contract must be approved to spend the USDC. The
//     "depositForBurn" method will emit a "MessageSent" event.
//  2. After a certain number of confirmations, use the hash of the message in
//     the emitted "MessageSent" event and retrieve an attestation from Circle's
//     API.
//  3. Use the message and attestation to call the "receiveMessage" function on the
//     target chain's "MessageTransmitter" contract to mint the USDC on the target
//     chain.
type usdcBridge struct {
	tokenMessenger         *cctp.TokenMessenger
	tokenMessengerAddr     common.Address
	messasgeTransmitter    *cctp.MessageTransmitter
	messageTransmitterAddr common.Address
	tokenContract          *erc20.IERC20
	tokenAddress           common.Address
	cb                     bind.ContractBackend
	attestationUrl         string
	net                    dex.Network
	addr                   common.Address
	node                   ethFetcher
}

var _ bridge = (*usdcBridge)(nil)

func newUsdcBridge(assetID uint32, net dex.Network, tokenAddress common.Address, cb bind.ContractBackend, addr common.Address, node ethFetcher) (*usdcBridge, error) {
	bridgeInfo, err := getUsdcBridgeInfo(assetID, net)
	if err != nil {
		return nil, err
	}

	attestationUrl, found := usdcBridgeAttestationUrl[net]
	if !found {
		return nil, fmt.Errorf("attestation url not found for network %s", net)
	}

	messageTransmitter, err := cctp.NewMessageTransmitter(bridgeInfo.messageTransmitterAddr, cb)
	if err != nil {
		return nil, err
	}

	tokenMessenger, err := cctp.NewTokenMessenger(bridgeInfo.tokenMessengerAddr, cb)
	if err != nil {
		return nil, err
	}

	tokenContract, err := erc20.NewIERC20(tokenAddress, cb)
	if err != nil {
		return nil, err
	}

	return &usdcBridge{
		tokenMessenger:         tokenMessenger,
		tokenMessengerAddr:     bridgeInfo.tokenMessengerAddr,
		messasgeTransmitter:    messageTransmitter,
		messageTransmitterAddr: bridgeInfo.messageTransmitterAddr,
		tokenAddress:           tokenAddress,
		tokenContract:          tokenContract,
		cb:                     cb,
		net:                    net,
		attestationUrl:         attestationUrl,
		addr:                   addr,
		node:                   node,
	}, nil
}

func (b *usdcBridge) bridgeContractAddr() common.Address {
	return b.tokenMessengerAddr
}

func (b *usdcBridge) bridgeContractAllowance(ctx context.Context) (*big.Int, error) {
	_, pendingUnavailable := b.cb.(*multiRPCClient)
	callOpts := &bind.CallOpts{
		Pending: !pendingUnavailable,
		From:    b.addr,
		Context: ctx,
	}
	return b.tokenContract.Allowance(callOpts, b.addr, b.tokenMessengerAddr)
}

func (b *usdcBridge) requiresBridgeContractApproval() bool {
	return true
}

func (b *usdcBridge) approveBridgeContract(txOpts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return b.tokenContract.Approve(txOpts, b.tokenMessengerAddr, amount)
}

func (b *usdcBridge) initiateBridge(txOpts *bind.TransactOpts, destAssetID uint32, amount *big.Int) (tx *types.Transaction, err error) {
	destBridgeInfo, err := getUsdcBridgeInfo(destAssetID, b.net)
	if err != nil {
		return nil, err
	}

	var recipient [32]byte
	copy(recipient[12:], b.addr[:])

	tx, err = b.tokenMessenger.DepositForBurn(txOpts, amount, destBridgeInfo.domainID, recipient, b.tokenAddress)
	if err != nil {
		return nil, err
	}

	return tx, nil
}

// usdcMintInfo is the data required to mint the USDC on the destination chain.
type usdcMintInfo struct {
	attestation []byte
	message     []byte
}

func (u *usdcMintInfo) serialize() []byte {
	return encode.BuildyBytes{0}.
		AddData(u.attestation).
		AddData(u.message)
}

func deserializeUsdcMintInfo(b []byte) (*usdcMintInfo, error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}

	if ver != 0 {
		return nil, fmt.Errorf("invalid version %d", ver)
	}

	if len(pushes) != 2 {
		return nil, fmt.Errorf("expected 2 pushes, got %d", len(pushes))
	}

	return &usdcMintInfo{
		attestation: pushes[0],
		message:     pushes[1],
	}, nil
}

// getMessageSentEventLog retrieves the MessageSent event log from the
// transaction receipt. This is required by circle's API to get the attestation.
func (b *usdcBridge) getMessageSentEventLog(ctx context.Context, bridgeTxID string) ([]byte, error) {
	receipt, err := b.node.transactionReceipt(ctx, common.HexToHash(bridgeTxID))
	if err != nil {
		return nil, fmt.Errorf("error getting transaction receipt: %w", err)
	}
	var msg []byte
	for _, log := range receipt.Logs {
		messageSent, err := b.messasgeTransmitter.ParseMessageSent(*log)
		if err != nil {
			continue
		}
		msg = messageSent.Message
		break
	}
	if msg == nil {
		return nil, fmt.Errorf("no message sent event found in the receipt")
	}

	return msg, nil
}

// getAttestation retrieves the attestation for the given message from Circle's
// API.
func (b *usdcBridge) getAttestation(ctx context.Context, msg []byte) ([]byte, error) {
	msgHash := "0x" + hex.EncodeToString(crypto.Keccak256(msg))
	url := fmt.Sprintf("%s%s", b.attestationUrl, msgHash)

	var attestationResponse struct {
		Attestation string `json:"attestation"`
		Status      string `json:"status"`
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := dexnet.Get(ctx, url, &attestationResponse); err != nil {
		return nil, err
	} else if attestationResponse.Status != "complete" {
		return nil, fmt.Errorf("attestation is still pending")
	}

	attestation := strings.TrimPrefix(attestationResponse.Attestation, "0x")
	return hex.DecodeString(attestation)
}

// getCompletionData retrieves the data required to complete the bridge.
func (b *usdcBridge) getCompletionData(ctx context.Context, bridgeTxID string) ([]byte, error) {
	msg, err := b.getMessageSentEventLog(ctx, bridgeTxID)
	if err != nil {
		return nil, err
	}

	attestation, err := b.getAttestation(ctx, msg)
	if err != nil {
		return nil, err
	}

	return (&usdcMintInfo{
		attestation: attestation,
		message:     msg,
	}).serialize(), nil
}

func (b *usdcBridge) completeBridge(txOpts *bind.TransactOpts, mintInfoB []byte) (*types.Transaction, error) {
	mintInfo, err := deserializeUsdcMintInfo(mintInfoB)
	if err != nil {
		return nil, err
	}

	if len(mintInfo.attestation) == 0 || len(mintInfo.message) == 0 {
		return nil, fmt.Errorf("invalid mint info")
	}

	return b.messasgeTransmitter.ReceiveMessage(txOpts, mintInfo.message, mintInfo.attestation)
}

func (b *usdcBridge) initiateBridgeGas() uint64 {
	// burn for deposit generally requires 102k-103k gas
	return 160_000
}

func (b *usdcBridge) completeBridgeGas() uint64 {
	// message received generally requires ~142k, but if this is the first
	// time the user owns this asset, it will be ~160k
	return 210_000
}

func (b *usdcBridge) requiresCompletion() bool {
	return true
}

func (b *usdcBridge) verifyBridgeCompletion(ctx context.Context, data []byte) (bool, error) {
	return false, fmt.Errorf("a completion transaction is for usdc")
}
