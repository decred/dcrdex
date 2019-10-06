// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpc

import (
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
	UnknownResponseID
)

const (
	JSONRPCVersion = "2.0"
)

// RPCError is returned as part of the Response to indicate that an error
// occured during method execution.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// NewRPCERror is a constructor for an RPCError.
func NewRPCError(code int, msg string) *RPCError {
	return &RPCError{
		Code:    code,
		Message: msg,
	}
}

// Request is the general form of a JSON-RPC request. The type of the Params field varies from one response to the next, so it is implemented as an interface{}.
type Request struct {
	// Jsonrpc must be exactly "2.0".
	Jsonrpc string `json:"jsonrpc"`
	// The name of the remote procedure being called.
	Method string `json:"method"`
	// Params can be any struct or array type, or nil. the type of Params varies
	// with method.
	Params json.RawMessage `json:"params"`
	// ID is a unique identifier the client assigns to this request. If the server
	// responds the the request, the Response will have the same ID field a
	// Request. Any integer, string or nil pointer is allowed for ID. A nil
	// pointer signifies that no response is needed, but may not be valid for all
	// requests.
	ID interface{} `json:"id"`
}

// ID64 attempts to parse the ID into an int64. The boolean return value will
// be false if the ID type is incompatible.
func (req *Request) ID64() (int64, bool) {
	return id64(req.ID)
}

// Message returns the Request wrapped in a Message.
func (req *Request) Message() (*Message, error) {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	return &Message{
		Type:    RequestMessage,
		Payload: json.RawMessage(reqBytes),
	}, nil
}

// Response is the general form of a JSON-RPC response.  The type of the
// Result field varies from one response to the next, so it is implemented as a
// json.RawMessage (alias of []byte). The ID field has to be a pointer to allow
// for a nil value when empty.
type Response struct {
	// Jsonrpc must be exactly "2.0".
	Jsonrpc string `json:"jsonrpc"`
	// Result is the payload, if successful, else nil.
	Result json.RawMessage `json:"result"`
	// Error is the RPCError if an errror was encountered, else nil.
	Error *RPCError `json:"error"`
	// ID is a unique identifier the client assigns to this request. If the server
	// responds the the request, the Response will have the same ID field a
	// Request. Any integer, string or nil pointer is allowed for ID. A nil
	// pointer signifies that no response is needed, but may not be valid for all
	// requests.
	ID *interface{} `json:"id"`
}

// NewResponse returns a new JSON-RPC response object. Though JSON-RPC
// specification dictates that either but not both of the result or the error
// should be nil, NewResponse does not check this condition.
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

// ID64 attempts to parse the ID into an int64. The boolean return value will
// be false if the type ID type is incompatible.
func (resp *Response) ID64() (int64, bool) {
	return id64(*resp.ID)
}

// Message returns the Request wrapped in a Message.
func (resp *Response) Message() (*Message, error) {
	respBytes, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	return &Message{
		Type:    ResponseMessage,
		Payload: json.RawMessage(respBytes),
	}, nil
}

// MessageType indicates the type of payload in a message.
const (
	InvalidMessage = iota
	ResponseMessage
	RequestMessage
)

// Message is the primary messaging type, and generally wraps a Response or
// a Request as it's payload. The Message wrapper is required to accomodate
// bi-directional requests on a single connection, i.e. the client can send
// Requests to the server, but the server can also send Requests to the client.
type Message struct {
	Type    uint8           `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// IsValidIDType checks that the ID field (which can go in any of the JSON-RPC
// requests, responses, or notifications) is valid.JSON-RPC 2.0 only allows
// string, number, or null, so this function restricts the allowed types to that
// list.
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

// id64 tries to parse the interface{} as an int64. Only numeric types will
// parse. If the type is non-numeric, the boolean return value will be false.
func id64(id interface{}) (int64, bool) {
	switch i := id.(type) {
	case int:
		return int64(i), true
	case int8:
		return int64(i), true
	case int16:
		return int64(i), true
	case int32:
		return int64(i), true
	case int64:
		return int64(i), true
	case uint:
		return int64(i), true
	case uint8:
		return int64(i), true
	case uint16:
		return int64(i), true
	case uint32:
		return int64(i), true
	case uint64:
		return int64(i), true
	case float32:
		return int64(i), true
	case float64:
		return int64(i), true
	}
	return -1, false
}
