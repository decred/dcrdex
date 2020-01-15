// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"encoding/json"
	"errors"
	"testing"

	"decred.org/dcrdex/dex/msgjson"
)

func verifyResponse(m *msgjson.Message, res interface{}, wantErrCode int) error {
	// using 1337 for all requests
	if m.ID != 1337 {
		return errors.New("wrong ID")
	}
	if m.Type != msgjson.Response {
		return errors.New("wrong Type")
	}
	payload := &msgjson.ResponsePayload{}
	if err := json.Unmarshal(m.Payload, payload); err != nil {
		return errors.New("unable to unmarshal payload")
	}
	if wantErrCode != 0 {
		if payload.Error.Code != wantErrCode {
			return errors.New("wrong error code")
		}
	}
	if err := json.Unmarshal(payload.Result, res); err != nil {
		return errors.New("unable to unmarshal res")
	}
	return nil
}

type Dummy struct {
	Status string
}

func TestCreateResponse(t *testing.T) {
	type createResTest struct {
		op          string
		res         interface{}
		resErr      *msgjson.Error
		wantErrCode int
	}
	tests := []createResTest{
		{
			"test one",
			Dummy{"ok"},
			nil,
			0,
		},
		{
			"test two",
			"",
			msgjson.NewError(msgjson.RPCParseError, "failed to encode response"),
			msgjson.RPCParseError,
		},
	}

	for _, test := range tests {
		msg, _ := createResponse(test.op, &test.res, test.resErr, 1337)
		if err := verifyResponse(&msg, &test.res, test.wantErrCode); err != nil {
			t.Error(err)
			return
		}

	}
}

func newRequest() *msgjson.Message {
	return &msgjson.Message{
		ID:   1337,
		Type: msgjson.Request,
	}
}

func TestHelp(t *testing.T) {
	msg := newRequest()
	handleHelp(nil, nil, msg)
	res := ""
	if err := verifyResponse(msg, &res, 0); err != nil {
		t.Error(err)
		return
	}
}

func TestVersion(t *testing.T) {
	msg := newRequest()
	handleVersion(nil, nil, msg)
	res := &VersionResult{}
	if err := verifyResponse(msg, res, 0); err != nil {
		t.Error(err)
		return
	}
}
