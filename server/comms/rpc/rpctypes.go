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
	JSONRPCVersion    = "2.0"
	MatchMethod       = "match"
	InitMethod        = "init"
	AuditMethod       = "audit"
	RedeemMethod      = "redeem"
	RedemptionMethod  = "redemption"
	RevokeMatchMethod = "revoke_match"
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

// AcknowledgementResult is the 'result' field in a RPCResponse to a request
// that requires an acknowledgement. It is typically a signature of some
// serialized data associated with the request.
type AcknowledgementResult struct {
	MatchID string `json:"matchid"`
	Sig     string `json:"sig"`
}

// RPCError is returned as part of an RPC response, and indicates that the
// the requested method did not succesfully execute.
type RPCError struct {
	Code    int
	Message string
}

// Request is the general form of a JSON-RPC request. The type of the Params
// field varies from one response to the next, so it is implemented as an
// interface{}.
type Request struct {
	// Jsonrpc must be exactly "2.0".
	Jsonrpc string `json:"jsonrpc"`
	// The name of the remote procedure being called.
	Method string `json:"method"`
	// Params can be any struct or array type, or nil. the type of Params varies
	// with method.
	Params json.RawMessage `json:"params"`
	// ID is a unique identifier the client assigns to this request. If the server
	// responds the the request, the Response will have the same ID field as
	// Request. Any integer, string or nil pointer is allowed for ID. A nil
	// pointer signifies that no response is needed, but may not be valid for all
	// requests.
	ID interface{} `json:"id"`
}

// NewRequest returns a new JSON-RPC request object given the provided id,
// method, and parameters.  The parameters are marshalled into a json.RawMessage
// for the Params field of the returned request object. This function is only
// provided in case the caller wants to construct raw requests for some reason.
// Typically callers will instead want to create a registered concrete command
// type with the NewCmd or New<Foo>Cmd functions and call the MarshalCmd
// function with that command to generate the marshalled JSON-RPC request.
func NewRequest(id interface{}, method string, params interface{}) (*Request, error) {
	if !IsValidIDType(id) {
		return nil, fmt.Errorf("the id of type '%T' is invalid", id)
	}

	marshalledParam, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}

	return &Request{
		Jsonrpc: JSONRPCVersion,
		ID:      id,
		Method:  method,
		Params:  json.RawMessage(marshalledParam),
	}, nil
}

// Response is the general form of a JSON-RPC response.  The type of the
// Result field varies from one response to the next, so it is implemented as an
// interface{}. The ID field has to be a pointer to allow for a nil value when
// empty.
type Response struct {
	Jsonrpc string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
	ID      *interface{}    `json:"id"`
}

// IsValidIDType checks that the ID field (which can go in any of the JSON-RPC
// requests, responses, or notifications) is valid.  JSON-RPC 1.0 allows any
// valid JSON type.  JSON-RPC 2.0 (which bitcoind follows for some parts) only
// allows string, number, or null, so this function restricts the allowed types
// to that list.  This function is only provided in case the caller is manually
// marshalling for some reason.    The functions which accept an ID in this
// package already call this function to ensure the provided id is valid.
func IsValidIDType(id interface{}) bool {
	switch id.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64,
		string,
		nil:
		return true
	default:
		return false
	}
}

// NewResponse returns a new JSON-RPC response object given the provided rpc
// version, id, marshalled result, and RPC error.  This function is only
// provided in case the caller wants to construct raw responses for some reason.
// Typically callers will instead want to create the fully marshalled JSON-RPC
// response to send over the wire with the MarshalResponse function.
func NewResponse(id interface{}, result interface{}, rpcErr *RPCError) (*Response, error) {
	if !IsValidIDType(id) {
		return nil, fmt.Errorf("the id of type '%T' is invalid", id)
	}

	marshalledResult, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}

	return &Response{
		Jsonrpc: JSONRPCVersion,
		Result:  marshalledResult,
		Error:   rpcErr,
		ID:      &id,
	}, nil
}

// MatchNotification is the params for a DEX-originating MatchMethod request.
type MatchNotification struct {
	signable
	OrderID  string `json:"orderid"`
	MatchID  string `json:"matchid"`
	Quantity uint64 `json:"quantity"`
	Rate     uint64 `json:"rate"`
	Address  string `json:"address"`
	Time     uint64 `json:"timestamp"`
}

// Check that MatchNotification satisfies Signable interface.
var _ Signable = (*MatchNotification)(nil)

// Serialize serializes the MatchNotification data.
func (params *MatchNotification) Serialize() ([]byte, error) {
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
	s = append(s, []byte(params.Address)...)
	return s, nil
}

// InitParams are the params for a client-originating InitMethod request.
type InitParams struct {
	signable
	OrderID  string `json:"orderid"`
	MatchID  string `json:"matchid"`
	TxID     string `json:"txid"`
	Vout     uint32 `json:"vout"`
	Time     uint64 `json:"timestamp"`
	Contract string `json:"contract"`
}

var _ Signable = (*InitParams)(nil)

// Serialize serializes the InitParams data.
func (params *InitParams) Serialize() ([]byte, error) {
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

// InitParams are the params for a DEX-originating AuditMethod request.
type AuditParams struct {
	signable
	OrderID  string `json:"orderid"`
	MatchID  string `json:"matchid"`
	Time     uint64 `json:"timestamp"`
	Contract string `json:"contract"`
}

var _ Signable = (*AuditParams)(nil)

// Serialize serializes the AuditParams data.
func (params *AuditParams) Serialize() ([]byte, error) {
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

// RevokeMatchParams are the params for a DEX-originating RevokeMatchMethod
// request.
type RevokeMatchParams struct {
	signable
	OrderID string `json:""`
	MatchID string `json:""`
}

var _ Signable = (*RevokeMatchParams)(nil)

// Serialize serializes the RevokeMatchParams data.
func (params *RevokeMatchParams) Serialize() ([]byte, error) {
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

// RedeemParams are the params for a client-originating RedeemMethod request.
type RedeemParams struct {
	signable
	OrderID string `json:"orderid"`
	MatchID string `json:"matchid"`
	TxID    string `json:"txid"`
	Vout    uint32 `json:"vout"`
	Time    uint64 `json:"timestamp"`
}

var _ Signable = (*RedeemParams)(nil)

// Serialize serializes the RedeemParams data.
func (params *RedeemParams) Serialize() ([]byte, error) {
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

// RedemptionParams are the params for a DEX-originating RedemptionMethod
// request. They are identical to RedeemParams, but RedeemParams are for the
// client-originating RedeemMethod request.
type RedemptionParams = RedeemParams

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
