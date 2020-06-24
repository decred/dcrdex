// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package msgjson

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"decred.org/dcrdex/dex/encode"

	"decred.org/dcrdex/dex"
)

// Error codes
const (
	RPCErrorUnspecified        = iota // 0
	RPCParseError                     // 1
	RPCUnknownRoute                   // 2
	RPCInternal                       // 3
	RPCQuarantineClient               // 4
	RPCVersionUnsupported             // 5
	RPCUnknownMatch                   // 6
	RPCInternalError                  // 7
	RPCInitError                      // 8
	RPCLoginError                     // 9
	RPCLogoutError                    // 10
	RPCCreateWalletError              // 11
	RPCOpenWalletError                // 12
	RPCWalletExistsError              // 13
	RPCCloseWalletError               // 14
	RPCGetFeeError                    // 15
	RPCRegisterError                  // 16
	RPCArgumentsError                 // 17
	RPCTradeError                     // 18
	RPCCancelError                    // 19
	RPCWithdrawError                  // 20
	SignatureError                    // 21
	SerializationError                // 22
	TransactionUndiscovered           // 23
	ContractError                     // 24
	SettlementSequenceError           // 25
	ResultLengthError                 // 26
	IDMismatchError                   // 27
	RedemptionError                   // 28
	IDTypeError                       // 29
	AckCountError                     // 30
	UnknownResponseID                 // 31
	OrderParameterError               // 32
	UnknownMarketError                // 33
	ClockRangeError                   // 34
	FundingError                      // 35
	CoinAuthError                     // 36
	UnknownMarket                     // 37
	NotSubscribedError                // 38
	UnauthorizedConnection            // 39
	AuthenticationError               // 40
	PubKeyParseError                  // 41
	FeeError                          // 42
	InvalidPreimage                   // 43
	PreimageCommitmentMismatch        // 44
	UnknownMessageType                // 45
	AccountClosedError                // 46
	MarketNotRunningError             // 47
	TryAgainLaterError                // 48
	AccountNotFoundError              // 49
)

// Routes are destinations for a "payload" of data. The type of data being
// delivered, and what kind of action is expected from the receiving party, is
// completely dependent on the route. The route designation is a string sent as
// the "route" parameter of a JSON-encoded Message.
const (
	// MatchRoute is the route of a DEX-originating request-type message notifying
	// the client of a match and initiating swap negotiation.
	MatchRoute = "match"
	// InitRoute is the route of a client-originating request-type message
	// notifying the DEX, and subsequently the match counter-party, of the details
	// of a swap contract.
	InitRoute = "init"
	// AuditRoute is the route of a DEX-originating request-type message relaying
	// swap contract details (from InitRoute) from one client to the other.
	AuditRoute = "audit"
	// RedeemRoute is the route of a client-originating request-type message
	// notifying the DEX, and subsequently the match counter-party, of the details
	// of a redemption transaction.
	RedeemRoute = "redeem"
	// RedemptionRoute is the route of a DEX-originating request-type message
	// relaying redemption transaction (from RedeemRoute) details from one client
	// to the other.
	RedemptionRoute = "redemption"
	// RevokeMatchRoute is a DEX-originating request-type message informing a
	// client that a match has been revoked.
	RevokeMatchRoute = "revoke_match"
	// LimitRoute is the client-originating request-type message placing a limit
	// order.
	LimitRoute = "limit"
	// MarketRoute is the client-originating request-type message placing a market
	// order.
	MarketRoute = "market"
	// CancelRoute is the client-originating request-type message placing a cancel
	// order.
	CancelRoute = "cancel"
	// OrderBookRoute is the client-originating request-type message subscribing
	// to an order book update notification feed.
	OrderBookRoute = "orderbook"
	// UnsubOrderBookRoute is client-originating request-type message cancelling
	// an order book subscription.
	UnsubOrderBookRoute = "unsub_orderbook"
	// BookOrderRoute is the DEX-originating notification-type message informing
	// the client to add the order to the order book.
	BookOrderRoute = "book_order"
	// UnbookOrderRoute is the DEX-originating notification-type message informing
	// the client to remove an order from the order book.
	UnbookOrderRoute = "unbook_order"
	// EpochOrderRoute is the DEX-originating notification-type message informing
	// the client about an order added to the epoch queue.
	EpochOrderRoute = "epoch_order"
	// UpdateRemainingRoute is the DEX-originating notification-type message that
	// updates the remaining amount of unfilled quantity on a standing limit order.
	UpdateRemainingRoute = "update_remaining"
	// ConnectRoute is a client-originating request-type message seeking
	// authentication so that the connection can be used for trading.
	ConnectRoute = "connect"
	// RegisterRoute is the client-originating request-type message initiating a
	// new client registration.
	RegisterRoute = "register"
	// ReinstateRoute is the client-originating request-type message reinstating a
	// client registration.
	ReinstateRoute = "reinstate"
	// NotifyFeeRoute is the client-originating request-type message informing the
	// DEX that the fee has been paid and has the requisite number of
	// confirmations.
	NotifyFeeRoute = "notifyfee"
	// ConfigRoute is the client-originating request-type message requesting the
	// DEX configuration information.
	ConfigRoute = "config"
	// MatchProofRoute is the DEX-originating notification-type message
	// delivering match cycle results to the client.
	MatchProofRoute = "match_proof"
	// PreimageRoute is the DEX-originating request-type message requesting the
	// preimages for the client's epoch orders.
	PreimageRoute = "preimage"
	// SuspensionRoute is the DEX-originating request-type message informing the
	// client of an upcoming trade suspension. This is part of the
	// subscription-based orderbook notification feed.
	SuspensionRoute = "suspension"
	// ResumptionRoute is the DEX-originating request-type message informing the
	// client of an upcoming trade resumption. This is part of the
	// subscription-based orderbook notification feed.
	ResumptionRoute = "resumption"
)

type Bytes = dex.Bytes

// Signable allows for serialization and signing.
type Signable interface {
	Serialize() []byte
	SetSig([]byte)
	SigBytes() []byte
}

// Signature partially implements Signable, and can be embedded by types intended
// to satisfy Signable, which must themselves implement the Serialize method.
type Signature struct {
	Sig Bytes `json:"sig"`
}

// SetSig sets the Sig field.
func (s *Signature) SetSig(b []byte) {
	s.Sig = b
}

// SigBytes returns the signature as a []byte.
func (s *Signature) SigBytes() []byte {
	// Assuming the Sig was set with SetSig, there is likely no way to error
	// here. Ignoring error for now.
	return s.Sig
}

// Stampable is an interface that supports timestamping and signing.
type Stampable interface {
	Signable
	Stamp(serverTime uint64)
}

// Acknowledgement is the 'result' field in a response to a request that
// requires an acknowledgement. It is typically a signature of some serialized
// data associated with the request.
type Acknowledgement struct {
	MatchID Bytes `json:"matchid"`
	Sig     Bytes `json:"sig"`
}

// Error is returned as part of the Response to indicate that an error
// occurred during method execution.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// String satisfies the Stringer interface for pretty printing.
func (e Error) String() string {
	return fmt.Sprintf("error code %d: %s", e.Code, e.Message)
}

// NewError is a constructor for an Error.
func NewError(code int, msg string) *Error {
	return &Error{
		Code:    code,
		Message: msg,
	}
}

// ResponsePayload is the payload for a Response-type Message.
type ResponsePayload struct {
	// Result is the payload, if successful, else nil.
	Result json.RawMessage `json:"result,omitempty"`
	// Error is the error, or nil if none was encountered.
	Error *Error `json:"error,omitempty"`
}

// MessageType indicates the type of message. MessageType is typically the first
// switch checked when examining a message, and how the rest of the message is
// decoded depends on its MessageType.
type MessageType uint8

const (
	InvalidMessageType MessageType = iota // 0
	Request                               // 1
	Response                              // 2
	Notification                          // 3
)

// String satisfies the Stringer interface for translating the MessageType code
// into a description, primarily for logging.
func (mt MessageType) String() string {
	switch mt {
	case Request:
		return "request"
	case Response:
		return "response"
	case Notification:
		return "notification"
	default:
		return "unknown MessageType"
	}
}

// Message is the primary messaging type for websocket communications.
type Message struct {
	// Type is the message type.
	Type MessageType `json:"type"`
	// Route is used for requests and notifications, and specifies a handler for
	// the message.
	Route string `json:"route,omitempty"`
	// ID is a unique number that is used to link a response to a request.
	ID uint64 `json:"id,omitempty"`
	// Payload is any data attached to the message. How Payload is decoded
	// depends on the Route.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// DecodeMessage decodes a *Message from JSON-formatted bytes.
func DecodeMessage(b []byte) (*Message, error) {
	msg := new(Message)
	err := json.Unmarshal(b, &msg)
	if err != nil {
		return nil, err
	}
	return msg, nil
}

// NewRequest is the constructor for a Request-type *Message.
func NewRequest(id uint64, route string, payload interface{}) (*Message, error) {
	if id == 0 {
		return nil, fmt.Errorf("id = 0 not allowed for a request-type message")
	}
	if route == "" {
		return nil, fmt.Errorf("empty string not allowed for route of request-type message")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Message{
		Type:    Request,
		Payload: json.RawMessage(encoded),
		Route:   route,
		ID:      id,
	}, nil
}

// NewResponse encodes the result and creates a Response-type *Message.
func NewResponse(id uint64, result interface{}, rpcErr *Error) (*Message, error) {
	if id == 0 {
		return nil, fmt.Errorf("id = 0 not allowed for response-type message")
	}
	encResult, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	resp := &ResponsePayload{
		Result: encResult,
		Error:  rpcErr,
	}
	encResp, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	return &Message{
		Type:    Response,
		Payload: json.RawMessage(encResp),
		ID:      id,
	}, nil
}

// Response attempts to decode the payload to a *ResponsePayload. Response will
// return an error if the Type is not Response.
func (msg *Message) Response() (*ResponsePayload, error) {
	if msg.Type != Response {
		return nil, fmt.Errorf("invalid type %d for ResponsePayload", msg.Type)
	}
	resp := new(ResponsePayload)
	err := json.Unmarshal(msg.Payload, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// NewNotification encodes the payload and creates a Notification-type *Message.
func NewNotification(route string, payload interface{}) (*Message, error) {
	if route == "" {
		return nil, fmt.Errorf("empty string not allowed for route of notification-type message")
	}
	encPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Message{
		Type:    Notification,
		Route:   route,
		Payload: json.RawMessage(encPayload),
	}, nil
}

// Unmarshal unmarshals the Payload field into the provided interface.
func (msg *Message) Unmarshal(payload interface{}) error {
	return json.Unmarshal(msg.Payload, payload)
}

// UnmarshalResult is a convenience method for decoding the Result field of a
// ResponsePayload.
func (msg *Message) UnmarshalResult(result interface{}) error {
	resp, err := msg.Response()
	if err != nil {
		return err
	}
	if resp.Error != nil {
		return fmt.Errorf("rpc error: %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return json.Unmarshal(resp.Result, result)
}

// String prints the message as a JSON-encoded string.
func (msg *Message) String() string {
	b, err := json.Marshal(msg)
	if err != nil {
		return "[Message decode error]"
	}
	return string(b)
}

// Match is the params for a DEX-originating MatchRoute request.
type Match struct {
	Signature
	OrderID    Bytes  `json:"orderid"`
	MatchID    Bytes  `json:"matchid"`
	Quantity   uint64 `json:"qty"`
	Rate       uint64 `json:"rate"`
	Address    string `json:"address"`
	ServerTime uint64 `json:"tserver"`
	// Status and Side are provided for convenience and are not part of the
	// match serialization.
	Status uint8 `json:"status"`
	Side   uint8 `json:"side"`
}

var _ Stampable = (*Match)(nil)

// Serialize serializes the Match data.
func (m *Match) Serialize() []byte {
	// Match serialization is orderid (32) + matchid (32) + quantity (8) + rate (8)
	// + server time (8) + address (variable, guess 35). Sum = 123
	s := make([]byte, 0, 123)
	s = append(s, m.OrderID...)
	s = append(s, m.MatchID...)
	s = append(s, uint64Bytes(m.Quantity)...)
	s = append(s, uint64Bytes(m.Rate)...)
	s = append(s, uint64Bytes(m.ServerTime)...)
	return append(s, []byte(m.Address)...)
}

// Stamp sets the server timestamp and epoch ID. Partially satisfies the
// Stampable interface.
func (m *Match) Stamp(t uint64) {
	m.ServerTime = t
}

// Init is the payload for a client-originating InitRoute request.
type Init struct {
	Signature
	OrderID Bytes `json:"orderid"`
	MatchID Bytes `json:"matchid"`
	CoinID  Bytes `json:"coinid"`
	// Time     uint64 `json:"timestamp"` // client originating
	Contract Bytes `json:"contract"`
}

var _ Signable = (*Init)(nil)

// Serialize serializes the Init data.
func (init *Init) Serialize() []byte {
	// Init serialization is orderid (32) + matchid (32) + txid (probably 32) +
	// vout (4) + contract (97 ish). Sum = 197
	s := make([]byte, 0, 197)
	s = append(s, init.OrderID...)
	s = append(s, init.MatchID...)
	s = append(s, init.CoinID...)
	//s = append(s, uint64Bytes(init.Time)...)
	return append(s, init.Contract...)
}

// Audit is the payload for a DEX-originating AuditRoute request.
type Audit struct {
	Signature
	OrderID  Bytes  `json:"orderid"`
	MatchID  Bytes  `json:"matchid"`
	Time     uint64 `json:"timestamp"`
	CoinID   Bytes  `json:"coinid"`
	Contract Bytes  `json:"contract"`
}

var _ Signable = (*Audit)(nil)

// Serialize serializes the Audit data.
func (audit *Audit) Serialize() []byte {
	// Audit serialization is orderid (32) + matchid (32) + time (8) +
	// coin ID (36) + contract (97 ish) = 205
	s := make([]byte, 0, 205)
	s = append(s, audit.OrderID...)
	s = append(s, audit.MatchID...)
	s = append(s, uint64Bytes(audit.Time)...)
	s = append(s, audit.CoinID...)
	return append(s, audit.Contract...)
}

// RevokeMatch are the params for a DEX-originating RevokeMatchRoute request.
type RevokeMatch struct {
	Signature
	OrderID Bytes `json:"orderid"`
	MatchID Bytes `json:"matchid"`
}

var _ Signable = (*RevokeMatch)(nil)

// Serialize serializes the RevokeMatchParams data.
func (rev *RevokeMatch) Serialize() []byte {
	// RevokeMatch serialization is order id (32) + match id (32) = 64 bytes
	s := make([]byte, 0, 64)
	s = append(s, rev.OrderID...)
	return append(s, rev.MatchID...)
}

// Redeem are the params for a client-originating RedeemRoute request.
type Redeem struct {
	Signature
	OrderID Bytes `json:"orderid"`
	MatchID Bytes `json:"matchid"`
	CoinID  Bytes `json:"coinid"`
	Secret  Bytes `json:"secret"`
	// Time    uint64 `json:"timestamp"`
}

var _ Signable = (*Redeem)(nil)

// Serialize serializes the Redeem data.
func (redeem *Redeem) Serialize() []byte {
	// Redeem serialization is orderid (32) + matchid (32) + coin ID (36) + secret
	// (32) = 132
	s := make([]byte, 0, 100)
	s = append(s, redeem.OrderID...)
	s = append(s, redeem.MatchID...)
	s = append(s, redeem.CoinID...)
	//s = append(s, uint64Bytes(redeem.Time)...)
	return append(s, redeem.Secret...)
}

// Redemption is the payload for a DEX-originating RedemptionRoute request.
type Redemption struct {
	Redeem
	Time uint64 `json:"timestamp"`
}

// Serialize serializes the Redemption data.
func (r *Redemption) Serialize() []byte {
	// Redemption serialization is Redeem (100) + timestamp (8) = 108
	s := r.Redeem.Serialize()
	return append(s, uint64Bytes(r.Time)...)
}

const (
	BuyOrderNum       = 1
	SellOrderNum      = 2
	StandingOrderNum  = 1
	ImmediateOrderNum = 2
	LimitOrderNum     = 1
	MarketOrderNum    = 2
	CancelOrderNum    = 3
)

// Coin is information for validating funding coins. Some number of
// Coins must be included with both Limit and Market payloads.
type Coin struct {
	ID      Bytes   `json:"coinid"`
	PubKeys []Bytes `json:"pubkeys"`
	Sigs    []Bytes `json:"sigs"`
	Redeem  Bytes   `json:"redeem"`
}

// Prefix is a common structure shared among order type payloads.
type Prefix struct {
	Signature
	AccountID  Bytes  `json:"accountid"`
	Base       uint32 `json:"base"`
	Quote      uint32 `json:"quote"`
	OrderType  uint8  `json:"ordertype"`
	ClientTime uint64 `json:"tclient"`
	ServerTime uint64 `json:"tserver"`
	Commit     Bytes  `json:"com"`
}

// Stamp sets the server timestamp and epoch ID. Partially satisfies the
// Stampable interface.
func (p *Prefix) Stamp(t uint64) {
	p.ServerTime = t
}

// TODO: Update prefix serialization with commitment.

// Serialize serializes the Prefix data.
func (p *Prefix) Serialize() []byte {
	// serialization: account ID (32) + base asset (4) + quote asset (4) +
	// order type (1) + client time (8) + server time (8) + commitment (32)
	// = 89 bytes
	b := make([]byte, 0, 89)
	b = append(b, p.AccountID...)
	b = append(b, uint32Bytes(p.Base)...)
	b = append(b, uint32Bytes(p.Quote)...)
	b = append(b, p.OrderType)
	b = append(b, uint64Bytes(p.ClientTime)...)
	b = append(b, uint64Bytes(p.ServerTime)...)
	return append(b, p.Commit...)
}

// Trade is common to Limit and Market Payloads.
type Trade struct {
	Side     uint8   `json:"side"`
	Quantity uint64  `json:"ordersize"`
	Coins    []*Coin `json:"coins"`
	Address  string  `json:"address"`
}

// Serialize serializes the Trade data.
func (t *Trade) Serialize() []byte {
	// serialization: coin count (1), coin data (36*count), side (1), qty (8)
	// = 10 + 36*count
	// Address is not serialized as part of the trade.
	coinCount := len(t.Coins)
	b := make([]byte, 0, 10+36*coinCount)
	b = append(b, byte(coinCount))
	for _, coin := range t.Coins {
		b = append(b, coin.ID...)
	}
	b = append(b, t.Side)
	return append(b, uint64Bytes(t.Quantity)...)
	// Note that Address is part of LimitOrder and MarketOrder serialization.
}

// LimitOrder is the payload for the LimitRoute, which places a limit order.
type LimitOrder struct {
	Prefix
	Trade
	Rate uint64 `json:"rate"`
	TiF  uint8  `json:"timeinforce"`
}

// Serialize serializes the Limit data.
func (l *LimitOrder) Serialize() []byte {
	// serialization: prefix (89) + trade (variable) + rate (8)
	// + time-in-force (1) + address (~35) = 133 + len(trade)
	trade := l.Trade.Serialize()
	b := make([]byte, 0, 133+len(trade))
	b = append(b, l.Prefix.Serialize()...)
	b = append(b, trade...)
	b = append(b, uint64Bytes(l.Rate)...)
	b = append(b, l.TiF)
	return append(b, []byte(l.Trade.Address)...)
}

// MarketOrder is the payload for the MarketRoute, which places a market order.
type MarketOrder struct {
	Prefix
	Trade
}

// Serialize serializes the MarketOrder data.
func (m *MarketOrder) Serialize() []byte {
	// serialization: prefix (89) + trade (varies) + address (35 ish)
	b := append(m.Prefix.Serialize(), m.Trade.Serialize()...)
	return append(b, []byte(m.Trade.Address)...)
}

// CancelOrder is the payload for the CancelRoute, which places a cancel order.
type CancelOrder struct {
	Prefix
	TargetID Bytes `json:"targetid"`
}

// Serialize serializes the CancelOrder data.
func (c *CancelOrder) Serialize() []byte {
	// serialization: prefix (89) + target id (32) = 121
	return append(c.Prefix.Serialize(), c.TargetID...)
}

// OrderResult is returned from the order-placing routes.
type OrderResult struct {
	Sig        Bytes  `json:"sig"`
	OrderID    Bytes  `json:"orderid"`
	ServerTime uint64 `json:"tserver"`
}

// OrderBookSubscription is the payload for a client-originating request to the
// OrderBookRoute, intializing an order book feed.
type OrderBookSubscription struct {
	Base  uint32 `json:"base"`
	Quote uint32 `json:"quote"`
}

// UnsubOrderBook is the payload for a client-originating request to the
// UnsubOrderBookRoute, terminating an order book subscription.
type UnsubOrderBook struct {
	MarketID string `json:"marketid"`
}

// orderbook subscription notification payloads include: BookOrderNote,
// UnbookOrderNote, EpochOrderNote, and MatchProofNote.

// OrderNote is part of a notification about any type of order.
type OrderNote struct {
	Seq      uint64 `json:"seq,omitempty"`      // May be empty when part of an OrderBook.
	MarketID string `json:"marketid,omitempty"` // May be empty when part of an OrderBook.
	OrderID  Bytes  `json:"oid"`
}

// TradeNote is part of a notification that includes information about a
// limit or market order.
type TradeNote struct {
	Side     uint8  `json:"side,omitempty"`
	Quantity uint64 `json:"qty,omitempty"`
	Rate     uint64 `json:"rate,omitempty"`
	TiF      uint8  `json:"tif,omitempty"`
	Time     uint64 `json:"time,omitempty"`
}

// BookOrderNote is the payload for a DEX-originating notification-type message
// informing the client to add the order to the order book.
type BookOrderNote struct {
	OrderNote
	TradeNote
}

// UnbookOrderRoute is the DEX-originating notification-type message informing
// the client to remove an order from the order book.
type UnbookOrderNote OrderNote

// EpochOrderRoute is the DEX-originating notification-type message informing
// the client about an order added to the epoch queue.
type EpochOrderNote struct {
	BookOrderNote
	Commit    Bytes  `json:"com"`
	OrderType uint8  `json:"otype"`
	Epoch     uint64 `json:"epoch"`
	TargetID  Bytes  `json:"target,omitempty"` // omit for cancel orders
}

// UpdateRemainingNote is the DEX-originating notification-type message
// informing the client about an update to a booked order's remaining quantity.
type UpdateRemainingNote struct {
	OrderNote
	Remaining uint64 `json:"remaining"`
}

// OrderBook is the response to a successful OrderBookSubscription.
type OrderBook struct {
	MarketID string `json:"marketid"`
	Seq      uint64 `json:"seq"`
	Epoch    uint64 `json:"epoch"`
	// MarketStatus `json:"status"`// maybe
	// DRAFT NOTE: We might want to use a different structure for bulk updates.
	// Sending a struct of arrays rather than an array of structs could
	// potentially cut the encoding effort and encoded size substantially.
	Orders []*BookOrderNote `json:"orders"`
}

// MatchProofNote is the match_proof notification payload.
type MatchProofNote struct {
	MarketID  string  `json:"marketid"`
	Epoch     uint64  `json:"epoch"`
	Preimages []Bytes `json:"preimages"`
	Misses    []Bytes `json:"misses"`
	CSum      Bytes   `json:"csum"`
	Seed      Bytes   `json:"seed"`
}

// TradeSuspension is the SuspensionRoute notification payload. It is part of
// the orderbook subscription.
type TradeSuspension struct {
	MarketID    string `json:"marketid"`
	FinalEpoch  uint64 `json:"finalepoch"`
	SuspendTime uint64 `json:"suspendtime"`
	Persist     bool   `json:"persistbook"`
}

// TradeResumption is the ResumptionRoute notification payload. It is part of
// the orderbook subscription. EpochLen is specified if the market configuration
// change, and the client should also hit the 'config' route for full details.
type TradeResumption struct {
	MarketID   string `json:"marketid"`
	StartEpoch uint64 `json:"startepoch"`
	EpochLen   uint64 `json:"epochlen,omitempty"` // maybe just ConfigChange bool `json:"configchange"`
}

// PreimageRequest is the server-originating preimage request payload.
type PreimageRequest struct {
	OrderID        Bytes `json:"orderid"`
	CommitChecksum Bytes `json:"csum"`
}

// PreimageResponse is the client-originating preimage response payload.
type PreimageResponse struct {
	Preimage Bytes `json:"pimg"`
}

// Connect is the payload for a client-originating ConnectRoute request.
type Connect struct {
	Signature
	AccountID  Bytes  `json:"accountid"`
	APIVersion uint16 `json:"apiver"`
	Time       uint64 `json:"timestamp"`
}

// Serialize serializes the Connect data.
func (c *Connect) Serialize() []byte {
	// serialization: account ID (32) + api version (2) + timestamp (8) = 42 bytes
	s := make([]byte, 0, 42)
	s = append(s, c.AccountID...)
	s = append(s, uint16Bytes(c.APIVersion)...)
	return append(s, uint64Bytes(c.Time)...)
}

// ConnectResult is the result result for the ConnectRoute request.
type ConnectResult struct {
	Sig     Bytes    `json:"sig"`
	Matches []*Match `json:"matches"`
}

// Register is the payload for the RegisterRoute request.
type Register struct {
	Signature
	PubKey Bytes  `json:"pubkey"`
	Time   uint64 `json:"timestamp"`
}

// Serialize serializes the Register data.
func (r *Register) Serialize() []byte {
	// serialization: pubkey (33) + time (8) = 41
	s := make([]byte, 0, 41)
	s = append(s, r.PubKey...)
	return append(s, uint64Bytes(r.Time)...)
}

// RegisterResult is the result for the response to Register.
type RegisterResult struct {
	Signature
	DEXPubKey    Bytes  `json:"pubkey"`
	ClientPubKey Bytes  `json:"-"`
	Address      string `json:"address"`
	Fee          uint64 `json:"fee"`
	Time         uint64 `json:"timestamp"`
}

// Serialize serializes the RegisterResult data.
func (r *RegisterResult) Serialize() []byte {
	// serialization: pubkey (33) + client pubkey (33) + time (8) + fee (8) +
	// address (35-ish) = 117
	b := make([]byte, 0, 117)
	b = append(b, r.DEXPubKey...)
	b = append(b, r.ClientPubKey...)
	b = append(b, uint64Bytes(r.Time)...)
	b = append(b, uint64Bytes(r.Fee)...)
	return append(b, []byte(r.Address)...)
}

// NotifyFee is the payload for a client-originating NotifyFeeRoute request.
type NotifyFee struct {
	Signature
	AccountID Bytes  `json:"accountid"`
	CoinID    Bytes  `json:"coinid"`
	Time      uint64 `json:"timestamp"`
}

// Serialize serializes the NotifyFee data.
func (n *NotifyFee) Serialize() []byte {
	// serialization: account id (32) + coinID (variable, ~32+2) + vout (4) +
	// time (8) = 78
	b := make([]byte, 0, 68)
	b = append(b, n.AccountID...)
	b = append(b, n.CoinID...)
	return append(b, uint64Bytes(n.Time)...)
}

// Reinstate is the payload for the ReinstateRoute request.
type Reinstate struct {
	Signature
	DEXPubKey    Bytes  `json:"dexpubkey"`
	ClientPubKey Bytes  `json:"clientpubkey"`
	AccountProof Bytes  `json:"accountproof"`
	CoinID       Bytes  `json:"coinid"`
	Time         uint64 `json:"timestamp"`
}

// Serialize serializes the Register data.
func (r *Reinstate) Serialize() []byte {
	// serialization: dexpubkey (33) + clientpubkey (33) +
	// accountproof 96 + coinID (variable, ~32+2) + time (8) = 204
	s := make([]byte, 0, 143)
	s = append(s, r.DEXPubKey...)
	s = append(s, r.ClientPubKey...)
	s = append(s, []byte(r.AccountProof)...)
	s = append(s, r.CoinID...)
	return append(s, uint64Bytes(r.Time)...)
}

// Account proof is information necessary to prove that the DEX server accepted
// the account's fee payment. The fee coin is not part of the proof, since it
// is already stored as part of the AccountInfo blob.
type AccountProof struct {
	Host  string
	Stamp uint64
	Sig   []byte
}

// Encode encodes the AccountProof to a versioned blob.
func (p *AccountProof) Encode() []byte {
	data := encode.BuildyBytes{0}.
		AddData([]byte(p.Host)).
		AddData(uint64Bytes(p.Stamp)).
		AddData(p.Sig)
	return data
}

// DecodeAccountProof decodes the versioned blob to a *MatchProof.
func DecodeAccountProof(b []byte) (*AccountProof, error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	switch ver {
	case 0:
		return decodeAccountProof_v0(pushes)
	}
	return nil, fmt.Errorf("unknown AccountProof version %d", ver)
}

func decodeAccountProof_v0(pushes [][]byte) (*AccountProof, error) {
	if len(pushes) != 3 {
		return nil, fmt.Errorf("decodeAccountProof_v0: expected 3 pushes, got %d", len(pushes))
	}
	hostB, stampB := pushes[0], pushes[1]
	return &AccountProof{
		Host:  string(hostB),
		Stamp: encode.IntCoder.Uint64(stampB),
		Sig:   pushes[2],
	}, nil
}

// Stamp satisfies the Stampable interface.
func (n *NotifyFee) Stamp(t uint64) {
	n.Time = t
}

// NotifyFeeResult is the result for the response to NotifyFee. Though it embeds
// Signature, it does not satisfy the Signable interface, as it has no need for
// serialization.
type NotifyFeeResult struct {
	Signature
}

// MarketStatus describes the status of the market, where StartEpoch is when the
// market started or will start. FinalEpoch is a when the market will suspend
// if it is running, or when the market suspended if it is presently stopped.
type MarketStatus struct {
	StartEpoch uint64 `json:"startepoch"`
	FinalEpoch uint64 `json:"finalepoch,omitempty"`
	Persist    *bool  `json:"persistbook,omitempty"` // nil and omitted when finalepoch is omitted
}

// Market describes a market and its variables, and is returned as part of a
// ConfigResult. The market's status (running, start epoch, and any planned
// final epoch before suspend) are also provided.
type Market struct {
	Name            string  `json:"name"`
	Base            uint32  `json:"base"`
	Quote           uint32  `json:"quote"`
	EpochLen        uint64  `json:"epochlen"`
	MarketBuyBuffer float64 `json:"buybuffer"`
	MarketStatus    `json:"status"`
}

// Asset describes an asset and its variables, and is returned as part of a
// ConfigResult.
type Asset struct {
	Symbol   string `json:"symbol"`
	ID       uint32 `json:"id"`
	LotSize  uint64 `json:"lotsize"`
	RateStep uint64 `json:"ratestep"`
	FeeRate  uint64 `json:"feerate"`
	SwapSize uint64 `json:"swapsize"`
	SwapConf uint16 `json:"swapconf"`
}

// ConfigResult is the successful result for the ConfigRoute.
type ConfigResult struct {
	CancelMax        float64   `json:"cancelmax"`
	BroadcastTimeout uint64    `json:"btimeout"`
	RegFeeConfirms   uint16    `json:"regfeeconfirms"`
	Assets           []*Asset  `json:"assets"`
	Markets          []*Market `json:"markets"`
	Fee              uint64    `json:"fee"`
}

// Convert uint64 to 8 bytes.
func uint64Bytes(i uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, i)
	return b
}

// Convert uint32 to 4 bytes.
func uint32Bytes(i uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, i)
	return b
}

// Convert uint32 to 4 bytes.
func uint16Bytes(i uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, i)
	return b
}
