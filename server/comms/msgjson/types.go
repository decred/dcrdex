// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package msgjson

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Error codes
const (
	RPCErrorUnspecified = iota
	RPCParseError
	RPCUnknownRoute
	RPCInternal
	RPCQuarantineClient
	RPCVersionUnsupported
	RPCUnknownMatch
	RPCInternalError
	SignatureError
	SerializationError
	TransactionUndiscovered
	ContractError
	SettlementSequenceError
	ResultLengthError
	IDMismatchError
	RedemptionError
	IDTypeError
	AckCountError
	UnknownResponseID
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
)

// Bytes is a byte slice that marshals to and unmarshals from a hexadecimal
// string. The default go behavior is to marshal []byte to a base-64 string.
type Bytes []byte

// BytesFromHex is a Bytes constructor that accepts a hex string.
func BytesFromHex(s string) (Bytes, error) {
	b, err := hex.DecodeString(s)
	return Bytes(b), err
}

// String return the hex encoding of the Bytes.
func (b Bytes) String() string {
	return hex.EncodeToString(b)
}

// MarshalJSON satisfies the json.Marshaller interface, and will marshal the
// bytes to a hex string.
func (b Bytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(hex.EncodeToString(b))
}

// UnmarshalJSON satisfies the json.Unmarshaler interface, and expects a UTF-8
// encoding of a hex string.
func (b *Bytes) UnmarshalJSON(encHex []byte) (err error) {
	if len(encHex) < 2 {
		return fmt.Errorf("marshalled Bytes, '%s', not valid", string(encHex))
	}
	*b, err = hex.DecodeString(string(encHex[1 : len(encHex)-1]))
	return err
}

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

// Match is the params for a DEX-originating MatchRoute request.
type Match struct {
	signable
	OrderID  Bytes  `json:"orderid"`
	MatchID  Bytes  `json:"matchid"`
	Quantity uint64 `json:"quantity"`
	Rate     uint64 `json:"rate"`
	Address  string `json:"address"`
	Time     uint64 `json:"timestamp"`
}

var _ Signable = (*Match)(nil)

// Serialize serializes the Match data.
func (m *Match) Serialize() ([]byte, error) {
	// Match serialization is orderid (32) + matchid (32) + quantity (8) + rate (8)
	// + time (8) + address (variable, guess 35). Sum = 123
	s := make([]byte, 0, 123)
	s = append(s, m.OrderID...)
	s = append(s, m.MatchID...)
	s = append(s, uint64Bytes(m.Quantity)...)
	s = append(s, uint64Bytes(m.Rate)...)
	s = append(s, uint64Bytes(m.Time)...)
	s = append(s, []byte(m.Address)...)
	return s, nil
}

// Init is the payload for a client-originating InitRoute request.
type Init struct {
	signable
	OrderID  Bytes  `json:"orderid"`
	MatchID  Bytes  `json:"matchid"`
	TxID     string `json:"txid"`
	Vout     uint32 `json:"vout"`
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
	s = append(s, []byte(init.TxID)...)
	s = append(s, uint32Bytes(init.Vout)...)
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
	TxID    string `json:"txid"`
	Vout    uint32 `json:"vout"`
	Time    uint64 `json:"timestamp"`
}

var _ Signable = (*Redeem)(nil)

// Serialize serializes the RedeemParams data.
func (redeem *Redeem) Serialize() ([]byte, error) {
	// Init serialization is orderid (32) + matchid (32) + txid (32) + vout (4)
	// + timestamp(8) = 108
	s := make([]byte, 0, 108)
	s = append(s, redeem.OrderID...)
	s = append(s, redeem.MatchID...)
	s = append(s, []byte(redeem.TxID)...)
	s = append(s, uint32Bytes(redeem.Vout)...)
	s = append(s, uint64Bytes(redeem.Time)...)
	return s, nil
}

// Redemption are the params for a DEX-originating RedemptionRoute request.
// They are identical to the Redeem parameters, but Redeem is for the
// client-originating RedeemRoute request.
type Redemption = Redeem

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
