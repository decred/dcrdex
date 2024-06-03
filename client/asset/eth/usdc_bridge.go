package eth

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/networks/erc20"
	"decred.org/dcrdex/dex/networks/erc20/cctp"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	usdcEthID, _     = dex.BipSymbolID("usdc.eth")
	usdcPolygonID, _ = dex.BipSymbolID("usdc.polygon")

	// burn for deposit generally requires 102k-103k gas
	burnForDepositGas uint64 = 160_000
	// message received generally requires ~142k, but if this is the first
	// time the user owns this asset, it will be ~160k
	messageReceivedGas uint64 = 210_000
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

func getUsdcBridgeInfo(assetID uint32, net dex.Network) *usdcBridgeInfo {
	assetBridgeInfo, found := usdcBridgeInfos[assetID]
	if !found {
		return nil
	}

	return assetBridgeInfo[net]
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
}

func newUsdcBrdge(assetID uint32, net dex.Network, tokenAddress common.Address, cb bind.ContractBackend) (*usdcBridge, error) {
	bridgeInfo := getUsdcBridgeInfo(assetID, net)
	if bridgeInfo == nil {
		return nil, fmt.Errorf("usdc bridge info not found for assetID %d and network %s", assetID, net)
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
	}, nil
}

// bridgeContractAddr returns the address of the bridge contract.
func (b *usdcBridge) bridgeContractAddr() common.Address {
	return b.tokenMessengerAddr
}

// bridgeContractAllowance returns the amount that the bridge contract is
// allowed to spend on behalf of the user.
func (b *usdcBridge) bridgeContractAllowance(ctx context.Context, addr common.Address) (*big.Int, error) {
	_, pendingUnavailable := b.cb.(*multiRPCClient)
	callOpts := &bind.CallOpts{
		Pending: !pendingUnavailable,
		From:    addr,
		Context: ctx,
	}
	return b.tokenContract.Allowance(callOpts, addr, b.tokenMessengerAddr)
}

// approveBridgeContract approves the bridge contract to spend the given amount
// of the token on behalf of the user.
func (b *usdcBridge) approveBridgeContract(txOpts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return b.tokenContract.Approve(txOpts, b.tokenMessengerAddr, amount)
}

// depositForBurn burns the given amount of the token on the source chain to
// bridge it to the destination chain.
func (b *usdcBridge) depositForBurn(txOpts *bind.TransactOpts, destAssetID uint32, addr common.Address, amount *big.Int) (*types.Transaction, error) {
	destBridgeInfo := getUsdcBridgeInfo(destAssetID, b.net)
	if destBridgeInfo == nil {
		return nil, fmt.Errorf("usdc bridge info not found for assetID %d and network %s", destAssetID, b.net)
	}

	var recipient [32]byte
	copy(recipient[12:], addr[:])

	return b.tokenMessenger.DepositForBurn(txOpts, amount, destBridgeInfo.domainID, recipient, b.tokenAddress)
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

// getMintInfo retrieves the message from the receipt of the depositForBurn
// call, and then retrieves the attestation from Circle's API. It returns the
// serialized data required to mint the USDC on the destination chain.
func (b *usdcBridge) getMintInfo(ctx context.Context, receipt *types.Receipt) ([]byte, error) {
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

	msgHash := "0x" + hex.EncodeToString(crypto.Keccak256(msg))
	url := fmt.Sprintf("%s%s", b.attestationUrl, msgHash)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("attestation request failed with status %d", resp.StatusCode)
	}

	reader := io.LimitReader(resp.Body, 1<<22)
	r, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	attestationResponse := struct {
		Attestation string `json:"attestation"`
		Status      string `json:"status"`
	}{}
	err = json.Unmarshal(r, &attestationResponse)
	if err != nil {
		return nil, err
	}

	if attestationResponse.Status != "complete" {
		return nil, fmt.Errorf("attestation is still pending")
	}

	attestation := strings.TrimPrefix(attestationResponse.Attestation, "0x")
	attestationBytes, err := hex.DecodeString(attestation)
	if err != nil {
		return nil, err
	}

	return (&usdcMintInfo{
		attestation: attestationBytes,
		message:     msg,
	}).serialize(), nil
}

// mintToken mints the USDC on the destination chain using the mint info.
func (b *usdcBridge) mintToken(txOpts *bind.TransactOpts, mintInfoB []byte) (*types.Transaction, error) {
	mintInfo, err := deserializeUsdcMintInfo(mintInfoB)
	if err != nil {
		return nil, err
	}

	return b.messasgeTransmitter.ReceiveMessage(txOpts, mintInfo.message, mintInfo.attestation)
}
