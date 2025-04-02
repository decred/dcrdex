// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mj

import (
	"encoding/json"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/feerates"
	"decred.org/dcrdex/dex/fiatrates"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/tatanka/tanka"
)

const (
	ErrNone = iota
	ErrTimeout
	ErrInternal
	ErrBadRequest
	ErrAuth
	ErrSig
	ErrNoConfig
	ErrBanned
	ErrFailedRelay
	ErrUnknownSender
	ErrCapacity
)

const (
	// tatanka <=> tatanka
	RouteTatankaConfig    = "tatanka_config"
	RouteTatankaConnect   = "tatanka_connect"
	RouteNewClient        = "new_client"
	RouteClientDisconnect = "client_disconnect"
	RouteRelayBroadcast   = "relay_broadcast"
	RouteRelayTankagram   = "relay_tankagram"
	RoutePathInquiry      = "path_inquiry"
	RouteShareScore       = "share_score"

	// tatanka <=> client
	RouteConnect             = "connect"
	RoutePostBond            = "post_bond"
	RouteSubscribe           = "subscribe"
	RouteUnsubscribe         = "unsubscribe"
	RouteUpdateSubscriptions = "update_subscriptions"
	RouteRates               = "rates"
	RouteSetScore            = "set_score"
	RouteFeeRateEstimate     = "fee_rate_estimate"

	// client1 <=> tatankanode <=> client2
	RouteTankagram     = "tankagram"
	RouteEncryptionKey = "encryption_key"
	RouteBroadcast     = "broadcast"
	RouteNewSubscriber = "new_subscriber"

	// HTTP Requests, used before client established WS connection
	RouteNodeInfo = "node_info"
)

const (
	TopicMarket          = "market"
	TopicFiatRate        = "fiat_rate"
	TopicFeeRateEstimate = "fee_rate_estimate"
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
	ID          tanka.PeerID                    `json:"id"`
	InitialSubs map[tanka.Topic][]tanka.Subject `json:"initialSubs"`
}

type UpdateSubscriptions struct {
	Subscriptions map[tanka.Topic][]tanka.Subject `json:"subscriptions"`
}

type Disconnect = Connect

// EncryptionKeyPayload is the payload of a Tankagram or TankagramResult that
// contains the ephemeral public key of the sender.
type EncryptionKeyPayload struct {
	EphemeralPubKey dex.Bytes `json:"ephemeralPubKey"`
}

type Tankagram struct {
	To               tanka.PeerID `json:"to"`
	From             tanka.PeerID `json:"from"`
	EncryptedPayload dex.Bytes    `json:"encryptedPayload"`
}

// TankagramError is a standard error that can be returned to a peer.
type TankagramError string

const (
	TEErrNone TankagramError = ""
	// TEEPeerError is returned when a peer encounters an error.
	TEEPeerError  TankagramError = "error"
	TEEBadRequest TankagramError = "bad_request"
	// TEDecryptionFailed is returned when a tankagram cannot be decrypted.
	// This usually means that the counterparty does not have the same ephemeral
	// encryption key, and it needs to be re-established.
	TEDecryptionFailed TankagramError = "decryption_failed"
)

// TankagramResultPayload must be the contents of a decrypted
// TankagramResult EncryptedResponse.
type TankagramResultPayload struct {
	Error   TankagramError `json:"error"`
	Payload dex.Bytes      `json:"payload"`
}

// TankagramResultType is a critical component of the mesh network's reputation
// system. The outcome of self-reported audits of counterparty failures will
// depend heavily on what signed TankagramResult.Result is submitted.
type TankagramResultType string

const (
	TRTTransmitted  TankagramResultType = "transmitted"
	TRTNoPath       TankagramResultType = "nopath"
	TRTErrFromTanka TankagramResultType = "errorfromtanka"
	TRTErrBadClient TankagramResultType = "badclient"
)

type TankagramResult struct {
	Result           TankagramResultType `json:"result"`
	EncryptedPayload dex.Bytes           `json:"encryptedPayload"`
}

type Broadcast struct {
	// TOOD: Why is PeerID part of the broadcast message when it can be
	// determined by the request?
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

type FeeRateEstimateMessage struct {
	Topic            tanka.Topic                   `json:"topic"`
	FeeRateEstimates map[uint32]*feerates.Estimate `json:"feeRateEstimates"`
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

// ScoreReport is an update of a client's score of a peer.
type ScoreReport struct {
	PeerID tanka.PeerID `json:"peerID"`
	Score  int8         `json:"score"`
}

// SharedScore is a scorer-scored tuple shared between tatanka nodes, along
// with the sending tatanka's new view of the scored peer's reputation.
type SharedScore struct {
	Scorer     tanka.PeerID      `json:"scorer"`
	Scored     tanka.PeerID      `json:"scored"`
	Score      int8              `json:"score"`
	Reputation *tanka.Reputation `json:"rep"`
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
