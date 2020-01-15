// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	//"decred.org/dcrdex/client/core"
	"encoding/json"
	"fmt"

	"decred.org/dcrdex/dex/msgjson"
)

// encodingFailurePayload is a message sent when encoding has failed. Set during
// initialization.
var encodingFailurePayload []byte

func init() {
	var err error
	msgjsonErr := msgjson.NewError(msgjson.RPCParseError, "failed to encode response")
	payload := &msgjson.ResponsePayload{Error: msgjsonErr}
	if encodingFailurePayload, err = json.Marshal(payload); err != nil {
		panic(err)
	}
}

// encode is a helper to marshal to JSON bytes.
func encode(op string, res interface{}) ([]byte, error) {
	encResult, err := json.Marshal(res)
	if err != nil {
		err := fmt.Errorf("unable to marshal data for %s: %v", op, err)
		log.Error(err)
		return nil, err
	}
	return encResult, nil
}

// decode is a helper to unmarshal JSON bytes to structs.
func decode(op string, b []byte, req interface{}) error {
	err := json.Unmarshal(b, req)
	if err != nil {
		err := fmt.Errorf("unable to unmarshal data for %s: %v", op, err)
		log.Error(err)
		return err
	}
	return nil
}

// createResponse create a msgjson response. The returned msgjson.Message is
// guaranteed to be a usable Message regardless of error.
func createResponse(op string, res interface{}, resErr *msgjson.Error, id uint64) (msgjson.Message, *msgjson.Error) {
	msg := msgjson.Message{
		Type: msgjson.Response,
		ID:   id,
	}

	// Encode result.
	encodedRes, err := encode(op, res)
	if err != nil {
		msg.Payload = encodingFailurePayload
		return msg, msgjson.NewError(msgjson.RPCParseError, err.Error())
	}

	// Place encoded result in a ResponsePayload and encode again.
	payload := &msgjson.ResponsePayload{Result: encodedRes, Error: resErr}
	encodedPayload, err := encode(op, payload)
	if err != nil {
		msg.Payload = encodingFailurePayload
		return msg, msgjson.NewError(msgjson.RPCParseError, err.Error())
	}
	msg.Payload = encodedPayload
	return msg, nil
}

// handle functions use the supplied request in msg and repopulate that msg with
// a response. msg will not be nil. handle functions may be called by RPC or
// websocket. If called by RPC, the response in msg will be sent by the caller
// of these functions. Websocket responses are sent if cl is supplied.

//RPC and Websocket

// handleHelp responds to the help request.
func handleHelp(s *RPCServer, cl *wsClient, msg *msgjson.Message) (msgjsonErr *msgjson.Error) {
	res := "help"
	*msg, msgjsonErr = createResponse(msg.Route, &res, msgjsonErr, msg.ID)
	if cl != nil {
		cl.Send(msg)
	}
	return

}

// handleVersion responds to the version request.
func handleVersion(s *RPCServer, cl *wsClient, msg *msgjson.Message) (msgjsonErr *msgjson.Error) {
	res := &VersionResult{
		Major: rpcSemverMajor,
		Minor: rpcSemverMinor,
		Patch: rpcSemverPatch,
	}
	*msg, msgjsonErr = createResponse(msg.Route, res, msgjsonErr, msg.ID)
	if cl != nil {
		cl.Send(msg)
	}
	return
}
