// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"encoding/json"
	"fmt"

	"decred.org/dcrdex/dex/msgjson"
)

// createResponse creates a msgjson response payload.
func createResponse(op string, res interface{}, resErr *msgjson.Error) *msgjson.ResponsePayload {
	encodedRes, err := json.Marshal(res)
	if err != nil {
		err := fmt.Errorf("unable to marshal data for %s: %v", op, err)
		panic(err)
	}
	return &msgjson.ResponsePayload{Result: encodedRes, Error: resErr}
}

//RPC and Websocket

// handleHelp responds to the help request.
func handleHelp(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	res := "help"
	return createResponse(req.Route, &res, nil)
}

// handleVersion responds to the version request.
func handleVersion(s *RPCServer, req *msgjson.Message) *msgjson.ResponsePayload {
	res := &VersionResult{
		Major: rpcSemverMajor,
		Minor: rpcSemverMinor,
		Patch: rpcSemverPatch,
	}
	return createResponse(req.Route, res, nil)
}
