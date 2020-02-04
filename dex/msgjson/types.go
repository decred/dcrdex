// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package msgjson

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"decred.org/dcrdex/dex"
)

// Error codes
const (
	RPCErrorUnspecified     = iota // 0
	RPCParseError                  // 1
	RPCUnknownRoute                // 2
	RPCInternal                    // 3
	RPCQuarantineClient            // 4
	RPCVersionUnsupported          // 5
	RPCUnknownMatch                // 6
	RPCInternalError               // 7
	SignatureError                 // 8
	SerializationError             // 9
	TransactionUndiscovered        // 10
	ContractError                  // 11
	SettlementSequenceError        // 12
	ResultLengthError              // 13
	IDMismatchError                // 14
	RedemptionError                // 15
	IDTypeError                    // 16
	AckCountError                  // 17
	UnknownResponseID              // 18
	OrderParameterError            // 19
	UnknownMarketError             // 20
	ClockRangeError                // 21
	FundingError                   // 22
	CoinAuthError                  // 23
	UnknownMarket                  // 24
	NotSubscribedError             // 25
	UnauthorizedConnection         // 26
	AuthenticationError            // 27
	PubKeyParseError               // 28
	FeeError                       // 29
	UnknownMessageType             // 30
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
	// ConnectRoute is a client-originating request-type message seeking
	// authentication so that the connection can be used for trading.
	ConnectRoute = "connect"
	// RegisterRoute is the client-originating request-type message initiating a
	// new client registration.
	RegisterRoute = "register"
	// NotifyFeeRoute is the client-originating request-type message informing the
	// DEX that the fee has been paid and has the requisite number of
	// confirmations.
	NotifyFeeRoute = "notifyfee"
	// ConfigRoute is the client-originating request-type message requesting the
	// DEX configuration information.
	ConfigRoute = "config"
	// MatchDataRoute is the DEX-originating request-type message delivering
	// match cycle info to the client.
	MatchDataRoute = "match_data"
	// MatchProofRoute is the DEX-originating request-type message delivering
	// match cycle results to the client.
	MatchProofRoute = "match_proof"
	// PreimageRoute is the DEX-originating request-type message requesting the
	// preimages for the client's epoch orders.
	PreimageRoute = "preimage"
	// SuspensionRoute is the DEX-originating request-type message informing the
	// client of an upcoming trade suspension.
	SuspensionRoute = "suspension"
)

type Bytes = dex.Bytes

// Signable allows for serialization and signing.
type Signable interface {
	Serialize() ([]byte, error)
	SetSig([]byte)
	SigBytes() []byte
}

// signable partially implements Signable, and can be embedded by types intended
// to satisfy Signable, which must themselves implement the Serialize method.
type signable struct {
	Sig Bytes `json:"sig"`
}

// SetSig sets the Sig field.
func (s *signable) SetSig(b []byte) {
	s.Sig = b
}

// SigBytes returns the signature as a []byte.
func (s *signable) SigBytes() []byte {
	// Assuming the Sig was set with SetSig, there is likely no way to error
	// here. Ignoring error for now.
	return s.Sig
}

// Stampable is an interface that supports timestamping and signing.
type Stampable interface {
	Signable
	Stamp(serverTime, epochIndex, epochDuration uint64)
}

// Acknowledgement is the 'result' field in a response to a request that
// requires an acknowledgement. It is typically a signature of some serialized
// data associated with the request.
type Acknowledgement struct {
	MatchID string `json:"matchid"`
	Sig     string `json:"sig"`
}

// Error is returned as part of the Response to indicate that an error
// occurred during method execution.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
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
		return nil, fmt.Errorf("empty string not allowed for route of request-type message")
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

// Match is the params for a DEX-originating MatchRoute request.
type Match struct {
	signable
	OrderID  Bytes  `json:"orderid"`
	MatchID  Bytes  `json:"matchid"`
	Quantity uint64 `json:"quantity"`
	Rate     uint64 `json:"rate"`
	Address  string `json:"address"`
	// Status and Side are provided for convenience and are not part of the
	// match serialization.
	Status uint8 `json:"status"`
	Side   uint8 `json:"side"`
}

var _ Signable = (*Match)(nil)

// Serialize serializes the Match data.
func (m *Match) Serialize() ([]byte, error) {
	// Match serialization is orderid (32) + matchid (32) + quantity (8) + rate (8)
	// + address (variable, guess 35). Sum = 115
	s := make([]byte, 0, 115)
	s = append(s, m.OrderID...)
	s = append(s, m.MatchID...)
	s = append(s, uint64Bytes(m.Quantity)...)
	s = append(s, uint64Bytes(m.Rate)...)
	s = append(s, []byte(m.Address)...)
	return s, nil
}

// Init is the payload for a client-originating InitRoute request.
type Init struct {
	signable
	OrderID  Bytes  `json:"orderid"`
	MatchID  Bytes  `json:"matchid"`
	CoinID   Bytes  `json:"coinid"`
	Time     uint64 `json:"timestamp"`
	Contract Bytes  `json:"contract"`
}

var _ Signable = (*Init)(nil)

// Serialize serializes the Init data.
func (init *Init) Serialize() ([]byte, error) {
	// Init serialization is orderid (32) + matchid (32) + txid (probably 32) +
	// vout (4) + timestamp (8) + contract (97 ish). Sum = 205
	s := make([]byte, 0, 205)
	s = append(s, init.OrderID...)
	s = append(s, init.MatchID...)
	s = append(s, init.CoinID...)
	s = append(s, uint64Bytes(init.Time)...)
	s = append(s, init.Contract...)
	return s, nil
}

// Audit is the payload for a DEX-originating AuditRoute request.
type Audit struct {
	signable
	OrderID  Bytes  `json:"orderid"`
	MatchID  Bytes  `json:"matchid"`
	Time     uint64 `json:"timestamp"`
	Contract Bytes  `json:"contract"`
}

var _ Signable = (*Audit)(nil)

// Serialize serializes the Audit data.
func (audit *Audit) Serialize() ([]byte, error) {
	// Audit serialization is orderid (32) + matchid (32) + time (8) +
	// contract (97 ish) = 169
	s := make([]byte, 0, 169)
	s = append(s, audit.OrderID...)
	s = append(s, audit.MatchID...)
	s = append(s, uint64Bytes(audit.Time)...)
	s = append(s, audit.Contract...)
	return s, nil
}

// RevokeMatch are the params for a DEX-originating RevokeMatchRoute request.
type RevokeMatch struct {
	signable
	OrderID Bytes `json:"orderid"`
	MatchID Bytes `json:"matchid"`
}

var _ Signable = (*RevokeMatch)(nil)

// Serialize serializes the RevokeMatchParams data.
func (rev *RevokeMatch) Serialize() ([]byte, error) {
	// RevokeMatch serialization is order id (32) + match id (32) = 64 bytes
	s := make([]byte, 0, 64)
	s = append(s, rev.OrderID...)
	s = append(s, rev.MatchID...)
	return s, nil
}

// Redeem are the params for a client-originating RedeemRoute request.
type Redeem struct {
	signable
	OrderID Bytes  `json:"orderid"`
	MatchID Bytes  `json:"matchid"`
	CoinID  Bytes  `json:"coinid"`
	Time    uint64 `json:"timestamp"`
}

var _ Signable = (*Redeem)(nil)

// Serialize serializes the Redeem data.
func (redeem *Redeem) Serialize() ([]byte, error) {
	// Init serialization is orderid (32) + matchid (32) + txid (32) + vout (4)
	// + timestamp(8) = 108
	s := make([]byte, 0, 108)
	s = append(s, redeem.OrderID...)
	s = append(s, redeem.MatchID...)
	s = append(s, []byte(redeem.CoinID)...)
	s = append(s, uint64Bytes(redeem.Time)...)
	return s, nil
}

// Redemption is the payload for a DEX-originating RedemptionRoute request.
// They are identical to the Redeem parameters, but Redeem is for the
// client-originating RedeemRoute request.
type Redemption = Redeem

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
	signable
	AccountID     Bytes  `json:"accountid"`
	Base          uint32 `json:"base"`
	Quote         uint32 `json:"quote"`
	OrderType     uint8  `json:"ordertype"`
	ClientTime    uint64 `json:"tclient"`
	ServerTime    uint64 `json:"tserver"`
	EpochIdx      uint64 `json:"epochidx"`
	EpochDuration uint64 `json:"epochdur"`
}

// Stamp sets the server timestamp and epoch ID. Partially satisfies the
// Stampable interface.
func (p *Prefix) Stamp(t, epochIdx, epochDur uint64) {
	p.ServerTime = t
	p.EpochIdx = epochIdx
	p.EpochDuration = epochDur
}

// TODO: Update prefix serialization with commitment.

// Serialize serializes the Prefix data.
func (p *Prefix) Serialize() []byte {
	// serialization: account ID (32) + base asset (4) + quote asset (4) +
	// order type (1), client time (8), server time (8), epoch ID (16) = 73 bytes
	b := make([]byte, 0, 73)
	b = append(b, p.AccountID...)
	b = append(b, uint32Bytes(p.Base)...)
	b = append(b, uint32Bytes(p.Quote)...)
	b = append(b, p.OrderType)
	b = append(b, uint64Bytes(p.ClientTime)...)
	b = append(b, uint64Bytes(p.ServerTime)...)
	b = append(b, uint64Bytes(p.EpochIdx)...)
	return append(b, uint64Bytes(p.EpochDuration)...)
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
}

// LimitOrder is the payload for the LimitRoute, which places a limit order.
type LimitOrder struct {
	Prefix
	Trade
	Rate uint64 `json:"rate"`
	TiF  uint8  `json:"timeinforce"`
}

// Serialize serializes the Limit data.
func (l *LimitOrder) Serialize() ([]byte, error) {
	// serialization: prefix (65) + trade (variable) + rate (8)
	// + time-in-force (1) + address (~35) = 110 + len(trade)
	trade := l.Trade.Serialize()
	b := make([]byte, 0, 110+len(trade))
	b = append(b, l.Prefix.Serialize()...)
	b = append(b, trade...)
	b = append(b, uint64Bytes(l.Rate)...)
	b = append(b, l.TiF)
	return append(b, []byte(l.Address)...), nil
}

// MarketOrder is the payload for the MarketRoute, which places a market order.
type MarketOrder struct {
	Prefix
	Trade
}

// Serialize serializes the MarketOrder data.
func (m *MarketOrder) Serialize() ([]byte, error) {
	// serialization: prefix (65) + trade (varies) + address (35 ish)
	b := append(m.Prefix.Serialize(), m.Trade.Serialize()...)
	return append(b, []byte(m.Address)...), nil
}

// CancelOrder is the payload for the CancelRoute, which places a cancel order.
type CancelOrder struct {
	Prefix
	TargetID Bytes `json:"targetid"`
}

// Serialize serializes the CancelOrder data.
func (c *CancelOrder) Serialize() ([]byte, error) {
	// serialization: prefix (57) + target id (32) = 89
	return append(c.Prefix.Serialize(), c.TargetID...), nil
}

// OrderResult is returned from the order-placing routes.
type OrderResult struct {
	Sig        Bytes  `json:"sig"`
	ServerTime uint64 `json:"tserver"`
	OrderID    Bytes  `json:"orderid"`
	EpochIdx   uint64 `json:"epochidx"`
	EpochDur   uint64 `json:"epochdur"`
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

// TradeNote is part of a notification that includes information about a
// limit or market order.
type TradeNote struct {
	Side     uint8  `json:"side,omitempty"`
	Quantity uint64 `json:"osize,omitempty"`
	Rate     uint64 `json:"rate,omitempty"`
	TiF      uint8  `json:"tif,omitempty"`
	Time     uint64 `json:"time,omitempty"`
}

// OrderNote is part of a notification about any type of order.
type OrderNote struct {
	Seq        uint64 `json:"seq,omitempty"`      // May be empty when part of an OrderBook.
	MarketID   string `json:"marketid,omitempty"` // May be empty when part of an OrderBook.
	OrderID    Bytes  `json:"oid"`
	Commitment Bytes  `json:"com"`
}

// BookOrderNote is the payload for a DEX-originating notification-type message
// informing the client to add the order to the order book.
type BookOrderNote struct {
	OrderNote
	TradeNote
}

// OrderBook is the response to a successful OrderBookSubscription.
type OrderBook struct {
	Seq      uint64 `json:"seq,omitempty"`
	MarketID string `json:"marketid"`
	// DRAFT NOTE: We might want to use a different structure for bulk updates.
	// Sending a struct of arrays rather than an array of structs could
	// potentially cut the encoding effort and encoded size substantially.
	Orders []*BookOrderNote `json:"orders"`
}

// UnbookOrderRoute is the DEX-originating notification-type message informing
// the client to remove an order from the order book.
type UnbookOrderNote OrderNote

// EpochOrderRoute is the DEX-originating notification-type message informing
// the client about an order added to the epoch queue.
type EpochOrderNote struct {
	BookOrderNote
	Commitment Bytes  `json:"com"`
	OrderType  uint8  `json:"otype"`
	TargetID   Bytes  `json:"target,omitempty"`
	Epoch      uint64 `json:"epoch"`
}

// Connect is the payload for a client-originating ConnectRoute request.
type Connect struct {
	signable
	AccountID  Bytes  `json:"accountid"`
	APIVersion uint16 `json:"apiver"`
	Time       uint64 `json:"timestamp"`
}

// Serialize serializes the Connect data.
func (c *Connect) Serialize() ([]byte, error) {
	// serialization: account ID (32) + api version (2) + timestamp (8) = 42 bytes
	s := make([]byte, 0, 42)
	s = append(s, c.AccountID...)
	s = append(s, uint16Bytes(c.APIVersion)...)
	s = append(s, uint64Bytes(c.Time)...)
	return s, nil
}

// Connect is the response result for the ConnectRoute request.
type ConnectResponse struct {
	Matches []*Match `json:"matches"`
}

// Register is the payload for the RegisterRoute request.
type Register struct {
	signable
	PubKey Bytes  `json:"pubkey"`
	Time   uint64 `json:"timestamp"`
}

// Serialize serializes the Register data.
func (r *Register) Serialize() ([]byte, error) {
	// serialization: pubkey (33) + time (8) = 41
	s := make([]byte, 0, 41)
	s = append(s, r.PubKey...)
	s = append(s, uint64Bytes(r.Time)...)
	return s, nil
}

// RegisterResult is the result for the response to Register.
type RegisterResult struct {
	signable
	DEXPubKey    Bytes  `json:"pubkey"`
	ClientPubKey Bytes  `json:"-"`
	Address      string `json:"address"`
	Fee          uint64 `json:"fee"`
	Time         uint64 `json:"timestamp"`
}

// Serialize serializes the RegisterResult data.
func (r *RegisterResult) Serialize() ([]byte, error) {
	// serialization: pubkey (33) + client pubkey (33) + time (8) + fee (8) +
	// address (35-ish) = 117
	b := make([]byte, 0, 117)
	b = append(b, r.DEXPubKey...)
	b = append(b, r.ClientPubKey...)
	b = append(b, uint64Bytes(r.Time)...)
	b = append(b, uint64Bytes(r.Fee)...)
	b = append(b, []byte(r.Address)...)
	return b, nil
}

// NotifyFee is the payload for a client-originating NotifyFeeRoute request.
type NotifyFee struct {
	signable
	AccountID Bytes  `json:"accountid"`
	CoinID    Bytes  `json:"coinid"`
	Vout      uint32 `json:"vout"`
	Time      uint64 `json:"timestamp"`
}

// Serialize serializes the NotifyFee data.
func (n *NotifyFee) Serialize() ([]byte, error) {
	// serialization: account id (32) + txid (32) + vout (4) + time (8) = 76
	b := make([]byte, 0, 68)
	b = append(b, n.AccountID...)
	b = append(b, n.CoinID...)
	b = append(b, uint32Bytes(n.Vout)...)
	b = append(b, uint64Bytes(n.Time)...)
	return b, nil
}

// NotifyFeeResult is the result for the response to NotifyFee. Though it embeds
//signable, it does not satisfy the Signable interface, as it has no need for
// serialization.
type NotifyFeeResult struct {
	signable
}

// ConfigResult is the successful result from the 'config' route.
type ConfigResult struct {
	CancelMax        float32  `json:"cancelmax"`
	BroadcastTimeout uint64   `json:"btimeout"`
	RegFeeConfirms   uint16   `json:"regfeeconfirms"`
	Assets           []Asset  `json:"assets"`
	Markets          []Market `json:"markets"`
}

// Market describes a market and its variables, and is returned as part of a
// ConfigResult.
type Market struct {
	Name            string  `json:"name"`
	Base            uint32  `json:"base"`
	Quote           uint32  `json:"quote"`
	EpochLen        uint64  `json:"epochlen"`
	StartEpoch      uint64  `json:"startepoch"`
	MarketBuyBuffer float64 `json:"buybuffer"`
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
	FundConf uint16 `json:"fundconf"`
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
