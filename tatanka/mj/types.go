// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mj

import (
	"crypto/sha256"
	"encoding/json"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/fiatrates"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/tatanka/tanka"
	"github.com/btcsuite/btcd/btcec/v2/ecdsa"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

const (
	ErrNone = iota
	ErrTimeout
	ErrInternal
	ErrBadRequest
	ErrAuth
	ErrSig
	ErrNoConfig
	ErrBannned
	ErrFailedRelay
)

const (
	// tatanka <=> tatanka
	RouteTatankaConfig    = "tatanka_config"
	RouteTatankaConnect   = "tatanka_connect"
	RouteNewClient        = "new_client"
	RouteClientDisconnect = "client_disconnect"
	RouteGetReputation    = "get_reputation"
	RouteRelayBroadcast   = "relay_broadcast"
	RouteRelayTankagram   = "relay_tankagram"
	RoutePathInquiry      = "path_inquiry"

	// tatanka <=> client
	RouteConnect     = "connect"
	RouteConfig      = "config"
	RoutePostBond    = "post_bond"
	RouteSubscribe   = "subscribe"
	RouteUnsubscribe = "unsubscribe"
	RouteRates       = "rates"

	// client1 <=> tatankanode <=> client2
	RouteTankagram     = "tankagram"
	RouteEncryptionKey = "encryption_key"
	RouteBroadcast     = "broadcast"
	RouteNewSubscriber = "new_subscriber"
)

const (
	TopicMarket   = "market"
	TopicFiatRate = "fiat_rate"
)

type BroadcastMessageType string

const (
	MessageTypeTrollBox      BroadcastMessageType = "troll_box"
	MessageTypeNewOrder      BroadcastMessageType = "new_order"
	MessageTypeProposeMatch  BroadcastMessageType = "propose_match"
	MessageTypeAcceptMatch   BroadcastMessageType = "accept_match"
	MessageTypeNewSubscriber BroadcastMessageType = "new_subscriber"
	MessageTypeUnsubTopic    BroadcastMessageType = "unsub_topic"
	MessageTypeUnsubSubject  BroadcastMessageType = "unsub_subject"
)

var msgIDCounter atomic.Uint64

func NewMessageID() uint64 {
	return msgIDCounter.Add(1)
}

type TatankaConfig struct {
	ID      tanka.PeerID `json:"id"`
	Version uint32       `json:"version"`
	Chains  []uint32     `json:"chains"`
	// BondTier is the senders current view of the receiver's tier.
	BondTier uint64 `json:"bondTier"`
}

type Connect struct {
	ID tanka.PeerID `json:"id"`
}

type Disconnect = Connect

type RemoteReputation struct {
	Score  int16
	NumPts uint8
}

type Tankagram struct {
	To           tanka.PeerID     `json:"to"`
	From         tanka.PeerID     `json:"from"`
	Message      *msgjson.Message `json:"message,omitempty"`
	EncryptedMsg dex.Bytes        `json:"encryptedMessage,omitempty"`
}

// TankagramResultType is a critical component of the mesh network's reputation
// system. The outcome of self-reported audits of counterparty failures will
// depend heavily on what signed TankagramResult.Result is submitted.
type TankagramResultType string

const (
	TRTTransmitted  TankagramResultType = "transmitted"
	TRTNoPath       TankagramResultType = "nopath"
	TRTErrFromPeer  TankagramResultType = "errorfrompeer"
	TRTErrFromTanka TankagramResultType = "errorfromtanka"
	TRTErrBadClient TankagramResultType = "badclient"
)

type TankagramResult struct {
	Result   TankagramResultType `json:"result"`
	Response dex.Bytes           `json:"response"`
	// If the tankagram is not transmitted, the server will stamp and sign the
	// tankagram separately.
	Stamp uint64    `json:"stamp"`
	Sig   dex.Bytes `json:"sig"`
}

func (r *TankagramResult) Sign(priv *secp256k1.PrivateKey) {
	r.Stamp = uint64(time.Now().UnixMilli())
	resultB := []byte(r.Result)
	preimage := make([]byte, len(resultB)+len(r.Response)+4)
	copy(preimage[:len(resultB)], resultB)
	copy(preimage[len(resultB):len(resultB)+len(r.Response)], r.Response)
	copy(preimage[len(resultB)+len(r.Response):], encode.Uint64Bytes(r.Stamp))
	digest := sha256.Sum256(preimage)
	r.Sig = ecdsa.Sign(priv, digest[:]).Serialize()
}

type Broadcast struct {
	PeerID      tanka.PeerID         `json:"peerID"`
	Topic       tanka.Topic          `json:"topic"`
	Subject     tanka.Subject        `json:"subject"`
	MessageType BroadcastMessageType `json:"messageType"`
	Payload     dex.Bytes            `json:"payload,omitempty"`
	Stamp       time.Time            `json:"stamp"`
}

type Subscription struct {
	Topic   tanka.Topic   `json:"topic"`
	Subject tanka.Subject `json:"subject"`
	// ChannelParameters json.RawMessage `json:"channelParameters,omitempty"`
}

type Unsubscription struct {
	Topic  tanka.Topic  `json:"topic"`
	PeerID tanka.PeerID `json:"peerID"`
}

type FundedMessage struct {
	AssetID uint32          `json:"assetID"`
	Funding json.RawMessage `json:"funding"`
	Msg     msgjson.Message `json:"msg"`
}

type RateMessage struct {
	Topic tanka.Topic                        `json:"topic"`
	Rates map[string]*fiatrates.FiatRateInfo `json:"rates"`
}

type Troll struct {
	Msg string `json:"msg"`
}

var ellipsis = []byte("...")

func Truncate(b []byte) string {
	const truncationLength = 300
	if len(b) <= truncationLength {
		return string(b)
	}
	truncated := make([]byte, truncationLength+len(ellipsis))
	copy(truncated[:truncationLength], b[:truncationLength])
	copy(truncated[truncationLength:], ellipsis)
	return string(truncated)
}

type PathInquiry = Connect

type NewSubscriber struct {
	PeerID  tanka.PeerID  `json:"peerID"`
	Topic   tanka.Topic   `json:"topic"`
	Subject tanka.Subject `json:"subject"`
}

func MustRequest(route string, payload any) *msgjson.Message {
	msg, err := msgjson.NewRequest(NewMessageID(), route, payload)
	if err != nil {
		panic("MustRequest error: " + err.Error())
	}
	return msg
}

func MustNotification(route string, payload any) *msgjson.Message {
	msg, err := msgjson.NewNotification(route, payload)
	if err != nil {
		panic("MustNotification error: " + err.Error())
	}
	return msg
}

func MustResponse(id uint64, payload any, rpcErr *msgjson.Error) *msgjson.Message {
	msg, err := msgjson.NewResponse(id, payload, rpcErr)
	if err != nil {
		panic("MustResponse error: " + err.Error())
	}
	return msg
}
