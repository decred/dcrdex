// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package msgjson

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/account"
)

// Error codes
const (
	RPCErrorUnspecified           = iota // 0
	RPCParseError                        // 1
	RPCUnknownRoute                      // 2
	RPCInternal                          // 3
	RPCQuarantineClient                  // 4
	RPCVersionUnsupported                // 5
	RPCUnknownMatch                      // 6
	RPCInternalError                     // 7
	RPCInitError                         // 8
	RPCLoginError                        // 9
	RPCLogoutError                       // 10
	RPCCreateWalletError                 // 11
	RPCOpenWalletError                   // 12
	RPCWalletExistsError                 // 13
	RPCCloseWalletError                  // 14
	RPCGetFeeError                       // 15, obsolete, kept for order
	RPCRegisterError                     // 16
	RPCArgumentsError                    // 17
	RPCTradeError                        // 18
	RPCCancelError                       // 19
	RPCFundTransferError                 // 20
	RPCOrderBookError                    // 21
	SignatureError                       // 22
	SerializationError                   // 23
	TransactionUndiscovered              // 24
	ContractError                        // 25
	SettlementSequenceError              // 26
	ResultLengthError                    // 27
	IDMismatchError                      // 28
	RedemptionError                      // 29
	IDTypeError                          // 30
	AckCountError                        // 31
	UnknownResponseID                    // 32
	OrderParameterError                  // 33
	UnknownMarketError                   // 34
	ClockRangeError                      // 35
	FundingError                         // 36
	CoinAuthError                        // 37
	UnknownMarket                        // 38
	NotSubscribedError                   // 39
	UnauthorizedConnection               // 40
	AuthenticationError                  // 41
	PubKeyParseError                     // 42
	FeeError                             // 43
	InvalidPreimage                      // 44
	PreimageCommitmentMismatch           // 45
	UnknownMessageType                   // 46
	AccountClosedError                   // 47
	MarketNotRunningError                // 48
	TryAgainLaterError                   // 49
	AccountNotFoundError                 // 50
	UnpaidAccountError                   // 51
	InvalidRequestError                  // 52
	OrderQuantityTooHigh                 // 53
	HTTPRouteError                       // 54
	RouteUnavailableError                // 55
	AccountExistsError                   // 56
	AccountSuspendedError                // 57 deprecated, kept for order
	RPCExportSeedError                   // 58
	TooManyRequestsError                 // 59
	RPCGetDEXConfigError                 // 60
	RPCDiscoverAcctError                 // 61
	RPCWalletRescanError                 // 62
	RPCDeleteArchivedRecordsError        // 63
	DuplicateRequestError                // 64
	RPCToggleWalletStatusError           // 65
	BondError                            // 66
	BondAlreadyConfirmingError           // 67
	RPCWalletPeersError                  // 68
	RPCNotificationsError                // 69
	RPCPostBondError                     // 70
	RPCWalletDefinitionError             // 71
)

// Routes are destinations for a "payload" of data. The type of data being
// delivered, and what kind of action is expected from the receiving party, is
// completely dependent on the route. The route designation is a string sent as
// the "route" parameter of a JSON-encoded Message.
const (
	// MatchRoute is the route of a DEX-originating request-type message notifying
	// the client of a match and initiating swap negotiation.
	MatchRoute = "match"
	// NoMatchRoute is the route of a DEX-originating notification-type message
	// notifying the client that an order did not match during its match cycle.
	NoMatchRoute = "nomatch"
	// MatchStatusRoute is the route of a client-originating request-type
	// message to retrieve match data from the DEX.
	MatchStatusRoute = "match_status"
	// OrderStatusRoute is the route of a client-originating request-type
	// message to retrieve order data from the DEX.
	OrderStatusRoute = "order_status"
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
	// RevokeMatchRoute is a DEX-originating notification-type message informing
	// a client that a match has been revoked.
	RevokeMatchRoute = "revoke_match"
	// RevokeOrderRoute is a DEX-originating notification-type message informing
	// a client that an order has been revoked.
	RevokeOrderRoute = "revoke_order"
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
	// EpochReportRoute is the DEX-originating notification-type message that
	// indicates the end of an epoch's book updates and provides stats for
	// maintaining a candlestick cache.
	EpochReportRoute = "epoch_report"
	// ConnectRoute is a client-originating request-type message seeking
	// authentication so that the connection can be used for trading.
	ConnectRoute = "connect"
	// RegisterRoute is the client-originating request-type message initiating a
	// new client registration. DEPRECATED with bonds (V0PURGE)
	RegisterRoute = "register"
	// NotifyFeeRoute is the client-originating request-type message informing the
	// DEX that the fee has been paid and has the requisite number of
	// confirmations. DEPRECATED with bonds (V0PURGE)
	NotifyFeeRoute = "notifyfee"
	// PostBondRoute is the client-originating request used to post a new
	// fidelity bond. This can create a new account or it can add bond to an
	// existing account.
	PostBondRoute = "postbond"
	// PreValidateBondRoute is the client-originating request used to
	// pre-validate a fidelity bond transaction before broadcasting it (and
	// locking funds for months).
	PreValidateBondRoute = "prevalidatebond"
	// BondExpiredRoute is a server-originating notification when a bond expires
	// according to the configure bond expiry duration and the bond's lock time.
	BondExpiredRoute = "bondexpired"
	// TierChangeRoute is a server-originating notification sent to a connected
	// user who's tier changes for any reason.
	TierChangeRoute = "tierchange" // (TODO: use in many auth mgr events)
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
	// NotifyRoute is the DEX-originating notification-type message
	// delivering text messages from the operator.
	NotifyRoute = "notify"
	// PenaltyRoute is the DEX-originating notification-type message
	// informing of a broken rule and the resulting penalty.
	PenaltyRoute = "penalty"
	// SpotsRoute is the client-originating HTTP or WebSocket request to get the
	// spot price and volume for the DEX's markets.
	SpotsRoute = "spots"
	// FeeRateRoute is the client-originating request asking for the most
	// recently recorded transaction fee estimate for an asset.
	FeeRateRoute = "fee_rate"
	// PriceFeedRoute is the client-originating request subscribing to the
	// market overview feed.
	PriceFeedRoute = "price_feed"
	// PriceUpdateRoute is a dex-originating notification updating the current
	// spot price for a market.
	PriceUpdateRoute = "price_update"
	// CandlesRoute is the HTTP request to get the set of candlesticks
	// representing market activity history.
	CandlesRoute = "candles"
)

const errNullRespPayload = dex.ErrorKind("null response payload")

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

// Error returns the error message. Satisfies the error interface.
func (e *Error) Error() string {
	return e.String()
}

// String satisfies the Stringer interface for pretty printing.
func (e Error) String() string {
	return fmt.Sprintf("error code %d: %s", e.Code, e.Message)
}

// NewError is a constructor for an Error.
func NewError(code int, format string, a ...interface{}) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, a...),
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

// There are presently three recognized message types: request, response, and
// notification.
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

// DecodeMessage decodes a *Message from JSON-formatted bytes. Note that
// *Message may be nil even if error is nil, when the message is JSON null,
// []byte("null").
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
		Payload: encoded,
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
		Payload: encResp,
		ID:      id,
	}, nil
}

// Response attempts to decode the payload to a *ResponsePayload. Response will
// return an error if the Type is not Response. It is an error if the Message's
// Payload is []byte("null").
func (msg *Message) Response() (*ResponsePayload, error) {
	if msg.Type != Response {
		return nil, fmt.Errorf("invalid type %d for ResponsePayload", msg.Type)
	}
	resp := new(ResponsePayload)
	err := json.Unmarshal(msg.Payload, &resp)
	if err != nil {
		return nil, err
	}
	if resp == nil /* null JSON */ {
		return nil, errNullRespPayload
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
		Payload: encPayload,
	}, nil
}

// Unmarshal unmarshals the Payload field into the provided interface. Note that
// the payload interface must contain a pointer. If it is a pointer to a
// pointer, it may become nil for a Message.Payload of []byte("null").
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
		return fmt.Errorf("rpc error: %w", resp.Error)
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
	OrderID      Bytes  `json:"orderid"`
	MatchID      Bytes  `json:"matchid"`
	Quantity     uint64 `json:"qty"`
	Rate         uint64 `json:"rate"`
	ServerTime   uint64 `json:"tserver"`
	Address      string `json:"address"`
	FeeRateBase  uint64 `json:"feeratebase"`
	FeeRateQuote uint64 `json:"feeratequote"`
	// Status and Side are provided for convenience and are not part of the
	// match serialization.
	Status uint8 `json:"status"`
	Side   uint8 `json:"side"`
}

var _ Signable = (*Match)(nil)

// Serialize serializes the Match data.
func (m *Match) Serialize() []byte {
	// Match serialization is orderid (32) + matchid (32) + quantity (8) + rate (8)
	// + server time (8) + address (variable, guess 35) + base fee rate (8) +
	// quote fee rate (8) = 139
	s := make([]byte, 0, 139)
	s = append(s, m.OrderID...)
	s = append(s, m.MatchID...)
	s = append(s, uint64Bytes(m.Quantity)...)
	s = append(s, uint64Bytes(m.Rate)...)
	s = append(s, uint64Bytes(m.ServerTime)...)
	s = append(s, []byte(m.Address)...)
	s = append(s, uint64Bytes(m.FeeRateBase)...)
	return append(s, uint64Bytes(m.FeeRateQuote)...)
}

// NoMatch is the payload for a server-originating NoMatchRoute notification.
type NoMatch struct {
	OrderID Bytes `json:"orderid"`
}

// MatchRequest details a match for the MatchStatusRoute request. The actual
// payload is a []MatchRequest.
type MatchRequest struct {
	Base    uint32 `json:"base"`
	Quote   uint32 `json:"quote"`
	MatchID Bytes  `json:"matchid"`
}

// MatchStatusResult is the successful result for the MatchStatusRoute request.
type MatchStatusResult struct {
	MatchID       Bytes `json:"matchid"`
	Status        uint8 `json:"status"`
	MakerContract Bytes `json:"makercontract,omitempty"`
	TakerContract Bytes `json:"takercontract,omitempty"`
	MakerSwap     Bytes `json:"makerswap,omitempty"`
	TakerSwap     Bytes `json:"takerswap,omitempty"`
	MakerRedeem   Bytes `json:"makerredeem,omitempty"`
	TakerRedeem   Bytes `json:"takerredeem,omitempty"`
	Secret        Bytes `json:"secret,omitempty"`
	Active        bool  `json:"active"`
	// MakerTxData and TakerTxData will only be populated by the server when the
	// match status is MakerSwapCast and TakerSwapCast, respectively.
	MakerTxData Bytes `json:"makertx,omitempty"`
	TakerTxData Bytes `json:"takertx,omitempty"`
}

// OrderStatusRequest details an order for the OrderStatusRoute request. The
// actual payload is a []OrderStatusRequest.
type OrderStatusRequest struct {
	Base    uint32 `json:"base"`
	Quote   uint32 `json:"quote"`
	OrderID Bytes  `json:"orderid"`
}

// OrderStatus is the current status of an order.
type OrderStatus struct {
	ID     Bytes  `json:"id"`
	Status uint16 `json:"status"`
}

// Init is the payload for a client-originating InitRoute request.
type Init struct {
	Signature
	OrderID  Bytes `json:"orderid"`
	MatchID  Bytes `json:"matchid"`
	CoinID   Bytes `json:"coinid"`
	Contract Bytes `json:"contract"`
}

var _ Signable = (*Init)(nil)

// Serialize serializes the Init data.
func (init *Init) Serialize() []byte {
	// Init serialization is orderid (32) + matchid (32) + coinid (probably 36)
	// + contract (97 ish). Sum = 197
	s := make([]byte, 0, 197)
	s = append(s, init.OrderID...)
	s = append(s, init.MatchID...)
	s = append(s, init.CoinID...)
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
	TxData   Bytes  `json:"txdata"`
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

// RevokeOrder are the params for a DEX-originating RevokeOrderRoute notification.
type RevokeOrder struct {
	Signature
	OrderID Bytes `json:"orderid"`
}

var _ Signable = (*RevokeMatch)(nil)

// Serialize serializes the RevokeOrder data.
func (rev *RevokeOrder) Serialize() []byte {
	// RevokeMatch serialization is order id (32) = 32 bytes
	s := make([]byte, 64)
	copy(s, rev.OrderID)
	return s
}

// RevokeMatch are the params for a DEX-originating RevokeMatchRoute request.
type RevokeMatch struct {
	Signature
	OrderID Bytes `json:"orderid"`
	MatchID Bytes `json:"matchid"`
}

var _ Signable = (*RevokeMatch)(nil)

// Serialize serializes the RevokeMatch data.
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
}

var _ Signable = (*Redeem)(nil)

// Serialize serializes the Redeem data.
func (redeem *Redeem) Serialize() []byte {
	// Redeem serialization is orderid (32) + matchid (32) + coin ID (36) +
	// secret (32) = 132
	s := make([]byte, 0, 132)
	s = append(s, redeem.OrderID...)
	s = append(s, redeem.MatchID...)
	s = append(s, redeem.CoinID...)
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

// Certain order properties are specified with the following constants. These
// properties include buy/sell (side), standing/immediate (force),
// limit/market/cancel (order type).
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
	// Note: ServerTime is zero for the client's signature message, but non-zero
	// for the server's. This is in contrast to an order.Order which cannot
	// even be serialized without the server's timestamp.
	b = append(b, uint64Bytes(p.ServerTime)...)
	return append(b, p.Commit...)
}

// Trade is common to Limit and Market Payloads.
type Trade struct {
	Side      uint8      `json:"side"`
	Quantity  uint64     `json:"ordersize"`
	Coins     []*Coin    `json:"coins"`
	Address   string     `json:"address"`
	RedeemSig *RedeemSig `json:"redeemsig,omitempty"` // account-based assets only. not serialized.
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

// RedeemSig is a signature proving ownership of the redeeming address. This is
// only necessary as part of a Trade if the asset received is account-based.
type RedeemSig struct {
	PubKey dex.Bytes `json:"pubkey"`
	Sig    dex.Bytes `json:"sig"`
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

// UnbookOrderNote is the DEX-originating notification-type message informing
// the client to remove an order from the order book.
type UnbookOrderNote OrderNote

// EpochOrderNote is the DEX-originating notification-type message informing the
// client about an order added to the epoch queue.
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
	Orders       []*BookOrderNote `json:"orders"`
	BaseFeeRate  uint64           `json:"baseFeeRate"`
	QuoteFeeRate uint64           `json:"quoteFeeRate"`
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
	Seq         uint64 `json:"seq,omitempty"`         // only set at suspend time and if Persist==false
	SuspendTime uint64 `json:"suspendtime,omitempty"` // only set in advance of suspend
	FinalEpoch  uint64 `json:"finalepoch"`
	Persist     bool   `json:"persistbook"`
}

// TradeResumption is the ResumptionRoute notification payload. It is part of
// the orderbook subscription.
type TradeResumption struct {
	MarketID   string `json:"marketid"`
	ResumeTime uint64 `json:"resumetime,omitempty"` // only set in advance of resume
	StartEpoch uint64 `json:"startepoch"`
	// TODO: ConfigChange bool or entire Config Market here.
}

// PreimageRequest is the server-originating preimage request payload.
type PreimageRequest struct {
	OrderID        Bytes `json:"orderid"`
	Commitment     Bytes `json:"commit"`
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

// Bond is information on a fidelity bond. This is part of the ConnectResult and
// PostBondResult payloads.
type Bond struct {
	Version uint16 `json:"version"`
	Amount  uint64 `json:"amount"`
	Expiry  uint64 `json:"expiry"` // when it expires, not the lock time
	CoinID  Bytes  `json:"coinID"` // NOTE: ID capitalization not consistent with other payloads, but internally consistent with assetID
	AssetID uint32 `json:"assetID"`
}

// ConnectResult is the result for the ConnectRoute request.
//
// TODO: Include penalty data as specified in the spec.
type ConnectResult struct {
	Sig                 Bytes          `json:"sig"`
	ActiveOrderStatuses []*OrderStatus `json:"activeorderstatuses"`
	ActiveMatches       []*Match       `json:"activematches"`
	Score               int32          `json:"score"`
	Tier                *int64         `json:"tier"` // 1+ means bonded and may trade, a function of active bond amounts and conduct, nil legacy
	ActiveBonds         []*Bond        `json:"activeBonds"`
	LegacyFeePaid       *bool          `json:"legacyFeePaid"` // not set by legacy server

	Suspended *bool `json:"suspended,omitempty"` // DEPRECATED - implied by tier<1
}

// TierChangedNotification is the dex-originating notification send when the
// user's tier changes as a result of account conduct violations. Tier change
// due to bond expiry is communicated with a BondExpiredNotification.
type TierChangedNotification struct {
	Signature
	// AccountID Bytes  `json:"accountID"`
	Tier   int64  `json:"tier"`
	Reason string `json:"reason"`
}

// Serialize serializes the TierChangedNotification data.
func (tc *TierChangedNotification) Serialize() []byte {
	// serialization: tier (8) + reason (variable string)
	b := make([]byte, 0, 8+len(tc.Reason))
	b = append(b, uint64Bytes(uint64(tc.Tier))...)
	return append(b, []byte(tc.Reason)...)
}

// PenaltyNote is the payload of a Penalty notification.
type PenaltyNote struct {
	Signature
	Penalty *Penalty `json:"penalty"`
}

// Penalty is part of the payload for a dex-originating Penalty notification
// and part of the connect response.
type Penalty struct {
	Rule     account.Rule `json:"rule"`
	Time     uint64       `json:"timestamp"`
	Duration uint64       `json:"duration,omitempty"` // DEPRECATED with bonding tiers, but must remain in serialization until v1 (V0PURGE)
	Details  string       `json:"details"`
}

// Serialize serializes the PenaltyNote data.
func (n *PenaltyNote) Serialize() []byte {
	p := n.Penalty
	// serialization: rule(1) + time (8) + duration (8) +
	// details (variable, ~100) = 117 bytes
	b := make([]byte, 0, 117)
	b = append(b, byte(p.Rule))
	b = append(b, uint64Bytes(p.Time)...)
	b = append(b, uint64Bytes(p.Duration)...)
	return append(b, []byte(p.Details)...)
}

// Client should send bond info when their bond tx is fully-confirmed. Server
// should start waiting for required confs when it receives the 'postbond'
// request if the txn is found. Client is responsible for submitting 'postbond'
// for their bond txns when they reach required confs. Implementation note: the
// client should also check on startup for stored bonds that are neither
// accepted nor expired yet (also maybe if not listed in the 'connect'
// response), and post those.

// PreValidateBond may provide the unsigned bond transaction for validation
// prior to broadcasting the signed transaction. If they skip pre-validation,
// and the broadcasted transaction is rejected, the client would have needlessly
// locked funds.
type PreValidateBond struct {
	Signature
	AcctPubKey Bytes  `json:"acctPubKey"` // acctID = blake256(blake256(acctPubKey))
	AssetID    uint32 `json:"assetID"`
	Version    uint16 `json:"version"`
	RawTx      Bytes  `json:"tx"`
	// Data       Bytes  `json:"data"` // needed for some assets? e.g. redeem script or contract key
}

// Serialize serializes the PreValidateBond data for the signature.
func (pb *PreValidateBond) Serialize() []byte {
	// serialization: client pubkey (33) + asset ID (4) + bond version (2) +
	// raw tx (variable)
	sz := len(pb.AcctPubKey) + 4 + 2 + len(pb.RawTx) // + len(pb.Data)
	b := make([]byte, 0, sz)
	b = append(b, pb.AcctPubKey...)
	b = append(b, uint32Bytes(pb.AssetID)...)
	b = append(b, uint16Bytes(pb.Version)...)
	return append(b, pb.RawTx...)
	// return append(b, pb.Data...)
}

// PreValidateBondResult is the response to the client's PreValidateBond
// request.
type PreValidateBondResult struct {
	Signature
	AccountID Bytes  `json:"accountID"`
	AssetID   uint32 `json:"assetID"`
	Amount    uint64 `json:"amount"`
	Expiry    uint64 `json:"expiry"` // not locktime, but time when bond expires for dex
	BondID    Bytes  `json:"bondID"`
}

// Serialize serializes the PreValidateBondResult data for the signature.
func (pbr *PreValidateBondResult) Serialize() []byte {
	sz := len(pbr.AccountID) + 4 + 8 + 8 + len(pbr.BondID)
	b := make([]byte, 0, sz)
	b = append(b, pbr.AccountID...)
	b = append(b, uint32Bytes(pbr.AssetID)...)
	b = append(b, uint64Bytes(pbr.Amount)...)
	b = append(b, uint64Bytes(pbr.Expiry)...)
	return append(b, pbr.BondID...)
}

// PostBond requests that server accept a confirmed bond payment, specified by
// the provided CoinID, for a certain account.
type PostBond struct {
	Signature
	AcctPubKey Bytes  `json:"acctPubKey"` // acctID = blake256(blake256(acctPubKey))
	AssetID    uint32 `json:"assetID"`
	Version    uint16 `json:"version"`
	CoinID     Bytes  `json:"coinid"`
	// For an account-based asset where there is a central bond contract implied
	// by Version, do we use AcctPubKey to lookup bonded amount, and CoinID to
	// wait for confs of this latest bond addition?
	// Data Bytes `json:"data"`
}

// Serialize serializes the PostBond data for the signature.
func (pb *PostBond) Serialize() []byte {
	// serialization: client pubkey (33) + asset ID (4) + bond version (2) +
	// coin ID (variable)
	sz := len(pb.AcctPubKey) + 4 + 2 + len(pb.CoinID)
	b := make([]byte, 0, sz)
	b = append(b, pb.AcctPubKey...)
	b = append(b, uint32Bytes(pb.AssetID)...)
	b = append(b, uint16Bytes(pb.Version)...)
	return append(b, pb.CoinID...)
}

// PostBondResult is the response to the client's PostBond request. If Active is
// true, the bond was applied to the account; if false it is not confirmed, but
// was otherwise validated.
type PostBondResult struct {
	Signature        // message is BondID | AccountID
	AccountID Bytes  `json:"accountID"`
	AssetID   uint32 `json:"assetID"`
	Amount    uint64 `json:"amount"`
	Expiry    uint64 `json:"expiry"` // not locktime, but time when bond expires for dex
	BondID    Bytes  `json:"bondID"`
	Tier      int64  `json:"tier"`
}

// Serialize serializes the PostBondResult data for the signature.
func (pbr *PostBondResult) Serialize() []byte {
	sz := len(pbr.AccountID) + len(pbr.BondID)
	b := make([]byte, 0, sz)
	b = append(b, pbr.AccountID...)
	return append(b, pbr.BondID...)
}

// BondExpiredNotification is a notification from a server when a bond tx
// expires.
type BondExpiredNotification struct {
	Signature
	AccountID  Bytes  `json:"accountID"`
	AssetID    uint32 `json:"assetid"`
	BondCoinID Bytes  `json:"coinid"`
	Tier       int64  `json:"tier"`
}

// Serialize serializes the BondExpiredNotification data.
func (bc *BondExpiredNotification) Serialize() []byte {
	sz := 4 + len(bc.AccountID) + 4 + len(bc.BondCoinID) + 8
	b := make([]byte, 0, sz)
	b = append(b, bc.AccountID...)
	b = append(b, uint32Bytes(bc.AssetID)...)
	b = append(b, bc.BondCoinID...)
	return append(b, uint64Bytes(uint64(bc.Tier))...) // correct bytes for int64 (signed)?
}

// Register is the payload for the RegisterRoute request.
type Register struct {
	Signature
	PubKey Bytes   `json:"pubkey"`
	Time   uint64  `json:"timestamp"`
	Asset  *uint32 `json:"feeAsset,omitempty"` // default to 42 if not set by client
}

// Serialize serializes the Register data.
func (r *Register) Serialize() []byte {
	// serialization: pubkey (33) + time (8) + asset (4 if set) = 45
	s := make([]byte, 0, 45)
	s = append(s, r.PubKey...)
	s = append(s, uint64Bytes(r.Time)...)
	if r.Asset != nil {
		s = append(s, uint32Bytes(*r.Asset)...)
	}
	return s
}

// RegisterResult is the result for the response to Register.
type RegisterResult struct {
	Signature
	DEXPubKey Bytes `json:"pubkey"`
	// ClientPubKey is excluded from the JSON payload to save bandwidth. The
	// client must add it back to verify the server's signature.
	ClientPubKey Bytes   `json:"-"`
	AssetID      *uint32 `json:"feeAsset,omitempty"` // default to 42 if not set by server
	Address      string  `json:"address"`
	Fee          uint64  `json:"fee"`
	Time         uint64  `json:"timestamp"`
}

// Serialize serializes the RegisterResult data.
func (r *RegisterResult) Serialize() []byte {
	// serialization: pubkey (33) + client pubkey (33) + time (8) + fee (8) +
	// address (35-ish) + asset (4 if set) = 121
	b := make([]byte, 0, 121)
	b = append(b, r.DEXPubKey...)
	b = append(b, r.ClientPubKey...)
	b = append(b, uint64Bytes(r.Time)...)
	b = append(b, uint64Bytes(r.Fee)...)
	b = append(b, []byte(r.Address)...)
	if r.AssetID != nil {
		b = append(b, uint32Bytes(*r.AssetID)...)
	}
	return b
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
	// serialization: account id (32) + coinID (variable, ~36) + time (8) = 76
	b := make([]byte, 0, 76)
	b = append(b, n.AccountID...)
	b = append(b, n.CoinID...)
	return append(b, uint64Bytes(n.Time)...)
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
	LotSize         uint64  `json:"lotsize"`
	RateStep        uint64  `json:"ratestep"`
	MarketBuyBuffer float64 `json:"buybuffer"`
	MarketStatus    `json:"status"`
}

// Running indicates if the market should be running given the known StartEpoch,
// EpochLen, and FinalEpoch (if set).
func (m *Market) Running() bool {
	dur := m.EpochLen
	now := uint64(time.Now().UnixMilli())
	start := m.StartEpoch * dur
	end := m.FinalEpoch * dur
	return now >= start && (now < end || end < start) // end < start detects obsolete end
}

// Asset describes an asset and its variables, and is returned as part of a
// ConfigResult.
type Asset struct {
	Symbol     string       `json:"symbol"`
	ID         uint32       `json:"id"`
	Version    uint32       `json:"version"`
	MaxFeeRate uint64       `json:"maxfeerate"`
	SwapConf   uint16       `json:"swapconf"`
	UnitInfo   dex.UnitInfo `json:"unitinfo"`

	// The swap/redeem size fields are DEPRECATED. They are implied by version.
	// The values provided by the server in these fields should not be used by
	// client wallets, which know the structure of their own transactions.
	SwapSize     uint64 `json:"swapsize"`
	SwapSizeBase uint64 `json:"swapsizebase"`
	RedeemSize   uint64 `json:"redeemsize"`

	// The Asset LotSize and RateStep fields are DEPRECATED. They are now
	// market specific.
	LotSize  uint64 `json:"lotsize,omitempty"`
	RateStep uint64 `json:"ratestep,omitempty"`
}

// FeeAsset describes an asset for which registration fees are supported.
type FeeAsset struct {
	ID    uint32 `json:"id"`
	Confs uint32 `json:"confs"`
	Amt   uint64 `json:"amount"`
}

// BondAsset describes an asset for which fidelity bonds are supported.
type BondAsset struct {
	Version uint16 `json:"version"` // latest version supported
	ID      uint32 `json:"id"`
	Confs   uint32 `json:"confs"`
	Amt     uint64 `json:"amount"` // to be implied by bond version?
}

// ConfigResult is the successful result for the ConfigRoute.
type ConfigResult struct {
	// APIVersion is the server's communications API version, but we may
	// consider APIVersions []uint16, with versioned routes e.g. "initV2".
	// APIVersions []uint16 `json:"apivers"`
	APIVersion       uint16    `json:"apiver"`
	DEXPubKey        dex.Bytes `json:"pubkey"`
	CancelMax        float64   `json:"cancelmax"`
	BroadcastTimeout uint64    `json:"btimeout"`
	Assets           []*Asset  `json:"assets"`
	Markets          []*Market `json:"markets"`
	BinSizes         []string  `json:"binSizes"` // Just apidata.BinSizes for now.

	BondAssets map[string]*BondAsset `json:"bondAssets"`
	// BondExpiry defines the duration of time remaining until lockTime below
	// which a bond is considered expired. As such, bonds should be created with
	// a considerably longer lockTime. NOTE: BondExpiry in the config response
	// is temporary, removed when APIVersion reaches BondAPIVersion and we have
	// codified the expiries for each network (main,test,sim). Until then, the
	// value will be considered variable, and we will communicate to the clients
	// what we expect at any given time. BondAsset.Amt may also become implied
	// by bond version.
	BondExpiry uint64 `json:"DEV_bondExpiry"`

	RegFees        map[string]*FeeAsset `json:"regFees"`
	Fee            uint64               `json:"fee"`            // DEPRECATED
	RegFeeConfirms uint16               `json:"regfeeconfirms"` // DEPRECATED
}

// Spot is a snapshot of a market at the end of a match cycle. A slice of Spot
// are sent as the response to the SpotsRoute request.
type Spot struct {
	Stamp      uint64  `json:"stamp"`
	BaseID     uint32  `json:"baseID"`
	QuoteID    uint32  `json:"quoteID"`
	Rate       uint64  `json:"rate"`
	BookVolume uint64  `json:"bookVolume"`
	Change24   float64 `json:"change24"`
	Vol24      uint64  `json:"vol24"`
	High24     uint64  `json:"high24"`
	Low24      uint64  `json:"low24"`
}

// CandlesRequest is a data API request for market history.
type CandlesRequest struct {
	BaseID     uint32 `json:"baseID"`
	QuoteID    uint32 `json:"quoteID"`
	BinSize    string `json:"binSize"`
	NumCandles int    `json:"numCandles,omitempty"` // default and max defined in apidata.
}

// Candle is a statistical history of a specified period of market activity.
type Candle struct {
	StartStamp  uint64 `json:"startStamp"`
	EndStamp    uint64 `json:"endStamp"`
	MatchVolume uint64 `json:"matchVolume"`
	QuoteVolume uint64 `json:"quoteVolume"`
	HighRate    uint64 `json:"highRate"`
	LowRate     uint64 `json:"lowRate"`
	StartRate   uint64 `json:"startRate"`
	EndRate     uint64 `json:"endRate"`
}

// WireCandles are Candles encoded as a series of integer arrays, as opposed to
// an array of candles. WireCandles encode smaller than []Candle, since the
// property names are not repeated for each candle.
type WireCandles struct {
	StartStamps  []uint64 `json:"startStamps"`
	EndStamps    []uint64 `json:"endStamps"`
	MatchVolumes []uint64 `json:"matchVolumes"`
	QuoteVolumes []uint64 `json:"quoteVolumes"`
	HighRates    []uint64 `json:"highRates"`
	LowRates     []uint64 `json:"lowRates"`
	StartRates   []uint64 `json:"startRates"`
	EndRates     []uint64 `json:"endRates"`
}

// NewWireCandles prepares a *WireCandles with slices of capacity n.
func NewWireCandles(n int) *WireCandles {
	return &WireCandles{
		StartStamps:  make([]uint64, 0, n),
		EndStamps:    make([]uint64, 0, n),
		MatchVolumes: make([]uint64, 0, n),
		QuoteVolumes: make([]uint64, 0, n),
		HighRates:    make([]uint64, 0, n),
		LowRates:     make([]uint64, 0, n),
		StartRates:   make([]uint64, 0, n),
		EndRates:     make([]uint64, 0, n),
	}
}

// Candles converts the WireCandles to []*Candle.
func (wc *WireCandles) Candles() []*Candle {
	candles := make([]*Candle, 0, len(wc.StartStamps))
	for i := range wc.StartStamps {
		candles = append(candles, &Candle{
			StartStamp:  wc.StartStamps[i],
			EndStamp:    wc.EndStamps[i],
			MatchVolume: wc.MatchVolumes[i],
			QuoteVolume: wc.QuoteVolumes[i],
			HighRate:    wc.HighRates[i],
			LowRate:     wc.LowRates[i],
			StartRate:   wc.StartRates[i],
			EndRate:     wc.EndRates[i],
		})
	}
	return candles
}

// EpochReportNote is a report about an epoch sent after all of the epoch's book
// updates. Like TradeResumption, and TradeSuspension when Persist is true, Seq
// is omitted since it doesn't modify the book.
type EpochReportNote struct {
	MarketID     string `json:"marketid"`
	Epoch        uint64 `json:"epoch"`
	BaseFeeRate  uint64 `json:"baseFeeRate"`
	QuoteFeeRate uint64 `json:"quoteFeeRate"`
	// MatchSummary: [rate, quantity]. Quantity is signed. Negative means that
	// the maker was a sell order.
	MatchSummary [][2]int64 `json:"matchSummary"`
	Candle
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
