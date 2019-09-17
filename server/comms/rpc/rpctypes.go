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
)

const (
	JSONRPCVersion = "2.0"
)

type RPCError struct {
	Code    int
	Message string
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
	// responds the the request, the Response will have the same ID field as
	// Request. Any integer, string or nil pointer is allowed for ID. A nil
	// pointer signifies that no response is needed, but may not be valid for all
	// requests.
	ID interface{} `json:"id"`
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
