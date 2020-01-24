// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"encoding/json"
	"errors"
	"testing"

	"decred.org/dcrdex/dex/msgjson"
)

func verifyResponse(payload *msgjson.ResponsePayload, res interface{}, wantErrCode int) error {
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
		payload := createResponse(test.op, &test.res, test.resErr)
		if err := verifyResponse(payload, &test.res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}

	}
}

var msg = new(msgjson.Message)

func TestHelp(t *testing.T) {
	payload := handleHelp(nil, msg)
	res := ""
	if err := verifyResponse(payload, &res, 0); err != nil {
		t.Fatal(err)
	}
}

func TestVersion(t *testing.T) {
	payload := handleVersion(nil, msg)
	res := &VersionResult{}
	if err := verifyResponse(payload, res, 0); err != nil {
		t.Fatal(err)
	}
}
