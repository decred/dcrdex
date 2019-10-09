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
