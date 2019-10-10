// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// The specifications for the Serialize methods can be found at
// https://github.com/buck54321/dcrdex/blob/p2sh-swap/spec/README.mediawiki#Match_negotiation
// DRAFT NOTE: Update this link ASAP.

package rpc

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Error codes
const (
	RPCParseError = iota
	RPCUnknownMethod
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
)

const (
	JSONRPCVersion   = "2.0"
	MatchRoute       = "match"
	InitRoute        = "init"
	AuditRoute       = "audit"
	RedeemRoute      = "redeem"
	RedemptionRoute  = "redemption"
	RevokeMatchRoute = "revoke_match"
)

// Signable allows for serialization and signing.
type Signable interface {
	Serialize() ([]byte, error)
	SetSig([]byte)
	SigBytes() []byte
}

// signable implements Signable, and should be embedded by any rpc type the
// is serializable and needs to encode a Sig field.
type signable struct {
	Sig string `json:"sig"`
}

// SetSig sets the Sig field.
func (s *signable) SetSig(b []byte) {
	s.Sig = hex.EncodeToString(b)
}

// SigBytes returns the signature as a []byte.
func (s *signable) SigBytes() []byte {
	// Assuming the Sig was set with SetSig, there is likely no way to error
	// here. Ignoring error for now.
	b, _ := hex.DecodeString(s.Sig)
	return b
}

// Acknowledgement is the 'result' field in a RPCResponse to a request that
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
	Result json.RawMessage `json:"result"`
	// Error is the error, or nil if none was encountered.
	Error *Error `json:"error"`
}

// MessageType indicates the type of message. MessageType is typically the first
// switch checked when examining a message, and how the rest of the message is
// decoded depends on its MessageType.
type MessageType uint8

const (
	InvalidMessageType MessageType = iota
	Request
	Response
	Notification
)

// Message is the primary messaging type for websocket communications.
type Message struct {
	// Type is the message type.
	Type MessageType `json:"type"`
	// Route is used for requests and notifications, and specifies a handler for
	// the message.
	Route string `json:"route,omitempty"`
	// ID is a unique number that is used to link a response to a request.
	ID uint64 `json:"id",omitempty`
	// Payload is any data attached to the message. How Payload is decoded
	// depends on the Route.
	Payload json.RawMessage `json:"payload",omitempty`
}

// DecodeMessage decodes a Message from bytes.
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

// MatchNotification is the params for a DEX-originating MatchMethod request.
type Match struct {
	signable
	OrderID  string `json:"orderid"`
	MatchID  string `json:"matchid"`
	Quantity uint64 `json:"quantity"`
	Rate     uint64 `json:"rate"`
	Address  string `json:"address"`
	Time     uint64 `json:"timestamp"`
}

// Check that Match satisfies Signable interface.
var _ Signable = (*Match)(nil)

// Serialize serializes the Match data.
func (params *Match) Serialize() ([]byte, error) {
	// Match serialization is orderid (32) + matchid (32) + quantity (8) + rate (8)
	// + address (variable, guess 35). Sum = 115
	s := make([]byte, 0, 91)

	// OrderID
	oid, err := hex.DecodeString(params.OrderID)
	if err != nil {
		return nil, fmt.Errorf("error decoding hex '%s': %v", params.OrderID, err)
	}
	s = append(s, oid...)

	// Everything else.
	matchID, err := hex.DecodeString(params.MatchID)
	if err != nil {
		return nil, fmt.Errorf("error decoding match ID %s: %v", params.MatchID, err)
	}
	s = append(s, matchID...)
	s = append(s, uint64Bytes(params.Quantity)...)
	s = append(s, uint64Bytes(params.Rate)...)
	s = append(s, uint64Bytes(params.Time)...)
	s = append(s, []byte(params.Address)...)
	return s, nil
}

// Init is the payload for a client-originating Init request.
type Init struct {
	signable
	OrderID  string `json:"orderid"`
	MatchID  string `json:"matchid"`
	TxID     string `json:"txid"`
	Vout     uint32 `json:"vout"`
	Time     uint64 `json:"timestamp"`
	Contract string `json:"contract"`
}

var _ Signable = (*Init)(nil)

// Serialize serializes the Init data.
func (params *Init) Serialize() ([]byte, error) {
	// Init serialization is orderid (32) + matchid (32) + txid (probably 64) +
	// vout (4) + timestamp (8) + contract (97 ish). Sum = 205
	s := make([]byte, 0, 237)

	oid, err := hex.DecodeString(params.OrderID)
	if err != nil {
		return nil, fmt.Errorf("error decoding order id '%s': %v", params.OrderID, err)
	}
	s = append(s, oid...)
	matchID, err := hex.DecodeString(params.MatchID)
	if err != nil {
		return nil, fmt.Errorf("error decoding match ID %s: %v", params.MatchID, err)
	}
	s = append(s, matchID...)
	txid := []byte(params.TxID)
	s = append(s, txid...)
	s = append(s, uint32Bytes(params.Vout)...)
	s = append(s, uint64Bytes(params.Time)...)
	contract, err := hex.DecodeString(params.Contract)
	if err != nil {
		return nil, fmt.Errorf("error decoding contract '%s': %v", params.Contract, err)
	}
	s = append(s, contract...)
	return s, nil
}

// Audit is the payload for a DEX-originating AuditRoute request.
type Audit struct {
	signable
	OrderID  string `json:"orderid"`
	MatchID  string `json:"matchid"`
	Time     uint64 `json:"timestamp"`
	Contract string `json:"contract"`
}

var _ Signable = (*Audit)(nil)

// Serialize serializes the AuditParams data.
func (params *Audit) Serialize() ([]byte, error) {
	// Audit serialization is orderid (32) + matchid (32) + time (8) +
	// contract (97 ish) = 145
	s := make([]byte, 0, 169)
	oid, err := hex.DecodeString(params.OrderID)
	if err != nil {
		return nil, fmt.Errorf("error decoding order id '%s': %v", params.OrderID, err)
	}
	s = append(s, oid...)
	matchID, err := hex.DecodeString(params.MatchID)
	if err != nil {
		return nil, fmt.Errorf("error decoding match ID %s: %v", params.MatchID, err)
	}
	s = append(s, matchID...)
	s = append(s, uint64Bytes(params.Time)...)
	contract, err := hex.DecodeString(params.Contract)
	if err != nil {
		return nil, fmt.Errorf("error decoding contract '%s': %v", params.Contract, err)
	}
	s = append(s, contract...)
	return s, nil
}

// RevokeMatch are the params for a DEX-originating RevokeMatchMethod
// request.
type RevokeMatch struct {
	signable
	OrderID string `json:""`
	MatchID string `json:""`
}

var _ Signable = (*RevokeMatch)(nil)

// Serialize serializes the RevokeMatchParams data.
func (params *RevokeMatch) Serialize() ([]byte, error) {
	// RevokeMatch serialization is order id (32) + match id (32) = 64 bytes
	s := make([]byte, 0, 64)
	oid, err := hex.DecodeString(params.OrderID)
	if err != nil {
		return nil, fmt.Errorf("error decoding order id '%s': %v", params.OrderID, err)
	}
	s = append(s, oid...)
	matchID, err := hex.DecodeString(params.MatchID)
	if err != nil {
		return nil, fmt.Errorf("error decoding match ID %s: %v", params.MatchID, err)
	}
	s = append(s, matchID...)
	return s, nil
}

// Redeem are the params for a client-originating RedeemMethod request.
type Redeem struct {
	signable
	OrderID string `json:"orderid"`
	MatchID string `json:"matchid"`
	TxID    string `json:"txid"`
	Vout    uint32 `json:"vout"`
	Time    uint64 `json:"timestamp"`
}

var _ Signable = (*Redeem)(nil)

// Serialize serializes the RedeemParams data.
func (params *Redeem) Serialize() ([]byte, error) {
	// Init serialization is orderid (32) + matchid (32) + txid (probably 64) +
	// vout (4) + timestamp (8) = 205
	s := make([]byte, 0, 140)

	oid, err := hex.DecodeString(params.OrderID)
	if err != nil {
		return nil, fmt.Errorf("error decoding order id '%s': %v", params.OrderID, err)
	}
	s = append(s, oid...)
	matchID, err := hex.DecodeString(params.MatchID)
	if err != nil {
		return nil, fmt.Errorf("error decoding match ID %s: %v", params.MatchID, err)
	}
	s = append(s, matchID...)
	txid := []byte(params.TxID)
	s = append(s, txid...)
	s = append(s, uint32Bytes(params.Vout)...)
	s = append(s, uint64Bytes(params.Time)...)
	return s, nil
}

// Redemption are the params for a DEX-originating RedemptionMethod request.
// They are identical to RedeemParams, but RedeemParams are for the
// client-originating RedeemMethod request.
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
