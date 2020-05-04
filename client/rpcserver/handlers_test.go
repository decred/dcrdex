// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
)

func verifyResponse(payload *msgjson.ResponsePayload, res interface{}, wantErrCode int) error {
	if wantErrCode != -1 {
		if payload.Error.Code != wantErrCode {
			return errors.New("wrong error code")
		}
	} else {
		if payload.Error != nil {
			return fmt.Errorf("unexpected error: %v", payload.Error)
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
	tests := []struct {
		name        string
		res         interface{}
		resErr      *msgjson.Error
		wantErrCode int
	}{{
		name:        "ok",
		res:         Dummy{"ok"},
		resErr:      nil,
		wantErrCode: -1,
	}, {
		name: "parse error",
		res:  "",
		resErr: msgjson.NewError(msgjson.RPCParseError,
			"failed to encode response"),
		wantErrCode: msgjson.RPCParseError,
	}}

	for _, test := range tests {
		payload := createResponse(test.name, &test.res, test.resErr)
		if err := verifyResponse(payload, &test.res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}

	}
}

func TestHelpMsgs(t *testing.T) {
	// routes and helpMsgs must have the same keys.
	if len(routes) != len(helpMsgs) {
		t.Fatal("routes and helpMsgs have different number of routes")
	}
	for k := range routes {
		if _, exists := helpMsgs[k]; !exists {
			t.Fatalf("%v exists in routes but not in helpMsgs", k)
		}
	}
}

func TestListCommands(t *testing.T) {
	// no passwords
	res := ListCommands(false)
	if res == "" {
		t.Fatal("unable to parse helpMsgs")
	}
	want := ""
	for _, r := range sortHelpKeys() {
		msg := helpMsgs[r]
		want += r + " " + msg.argsShort + "\n"
	}
	if res != want[:len(want)-1] {
		t.Fatalf("wanted %s but got %s", want, res)
	}
	// with passwords
	res = ListCommands(true)
	if res == "" {
		t.Fatal("unable to parse helpMsgs")
	}
	want = ""
	for _, r := range sortHelpKeys() {
		msg := helpMsgs[r]
		if msg.pwArgsShort != "" {
			want += r + " " + format(msg.pwArgsShort, " ") + msg.argsShort + "\n"
		} else {
			want += r + " " + msg.argsShort + "\n"
		}
	}
	if res != want[:len(want)-1] {
		t.Fatalf("wanted %s but got %s", want, res)
	}
}

func TestCommandUsage(t *testing.T) {
	for r, msg := range helpMsgs {
		// no passwords
		res, err := commandUsage(r, false)
		if err != nil {
			t.Fatalf("unexpected error for command %s", r)
		}
		want := r + " " + msg.argsShort + "\n\n" + msg.cmdSummary + "\n\n" +
			format(msg.argsLong, "\n\n") + msg.returns
		if res != want {
			t.Fatalf("wanted %s but got %s for usage of %s without passwords", want, res, r)
		}

		// with passwords when applicable
		if msg.pwArgsShort != "" {
			res, err = commandUsage(r, true)
			if err != nil {
				t.Fatalf("unexpected error for command %s", r)
			}
			want = r + " " + format(msg.pwArgsShort, " ") + msg.argsShort + "\n\n" +
				msg.cmdSummary + "\n\n" + format(msg.pwArgsLong, "\n\n") +
				format(msg.argsLong, "\n\n") + msg.returns
			if res != want {
				t.Fatalf("wanted %s but got %s for usage of %s with passwords", want, res, r)
			}
		}
	}
	if _, err := commandUsage("never make this command", false); !errors.Is(err, errUnknownCmd) {
		t.Fatal("expected error for bogus command")
	}
}

func TestHandleHelp(t *testing.T) {
	tests := []struct {
		name        string
		params      *RawParams
		wantErrCode int
	}{{
		name:        "ok no arg",
		params:      new(RawParams),
		wantErrCode: -1,
	}, {
		name:        "ok with arg",
		params:      &RawParams{Args: []string{"version"}},
		wantErrCode: -1,
	}, {
		name:        "unknown route",
		params:      &RawParams{Args: []string{"versio"}},
		wantErrCode: msgjson.RPCUnknownRoute,
	}, {
		name:        "bad params",
		params:      &RawParams{Args: []string{"version", "blue"}},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		payload := handleHelp(nil, test.params)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleVersion(t *testing.T) {
	payload := handleVersion(nil, nil)
	res := ""
	if err := verifyResponse(payload, &res, -1); err != nil {
		t.Fatal(err)
	}
}

func TestHandlePreRegister(t *testing.T) {
	tests := []struct {
		name           string
		params         *RawParams
		preRegisterFee uint64
		preRegisterErr error
		wantErrCode    int
	}{{
		name:           "ok",
		params:         &RawParams{Args: []string{"dex", "cert"}},
		preRegisterFee: 5,
		wantErrCode:    -1,
	}, {
		name:           "core.PreRegister error",
		params:         &RawParams{Args: []string{"dex", "cert"}},
		preRegisterErr: errors.New("error"),
		wantErrCode:    msgjson.RPCPreRegisterError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			preRegisterFee: test.preRegisterFee,
			preRegisterErr: test.preRegisterErr,
		}
		r := &RPCServer{core: tc}
		payload := handlePreRegister(r, test.params)
		res := new(preRegisterResponse)
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
		if test.wantErrCode == -1 && res.Fee != test.preRegisterFee {
			t.Fatalf("wanted registration fee %d but got %d for test %s",
				test.preRegisterFee, res.Fee, test.name)
		}
	}
}

func TestHandleInit(t *testing.T) {
	pw := encode.PassBytes("password123")
	tests := []struct {
		name                string
		params              *RawParams
		initializeClientErr error
		wantErrCode         int
	}{{
		name:        "ok",
		params:      &RawParams{PWArgs: []encode.PassBytes{pw}},
		wantErrCode: -1,
	}, {
		name:                "core.InitializeClient error",
		params:              &RawParams{PWArgs: []encode.PassBytes{pw}},
		initializeClientErr: errors.New("error"),
		wantErrCode:         msgjson.RPCInitError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{initializeClientErr: test.initializeClientErr}
		r := &RPCServer{core: tc}
		payload := handleInit(r, test.params)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleNewWallet(t *testing.T) {
	pw := encode.PassBytes("password123")
	params := &RawParams{
		PWArgs: []encode.PassBytes{pw, pw},
		Args: []string{
			"42",
			"default",
			"/home/wallet.conf",
		},
	}
	tests := []struct {
		name            string
		params          *RawParams
		walletState     *core.WalletState
		createWalletErr error
		openWalletErr   error
		wantErrCode     int
	}{{
		name:        "ok new wallet",
		params:      params,
		wantErrCode: -1,
	}, {
		name:        "ok existing wallet",
		params:      params,
		walletState: &core.WalletState{Open: false},
		wantErrCode: msgjson.RPCWalletExistsError,
	}, {
		name:            "core.CreateWallet error",
		params:          params,
		createWalletErr: errors.New("error"),
		wantErrCode:     msgjson.RPCCreateWalletError,
	}, {
		name:          "core.OpenWallet error",
		params:        params,
		openWalletErr: errors.New("error"),
		wantErrCode:   msgjson.RPCOpenWalletError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			walletState:     test.walletState,
			createWalletErr: test.createWalletErr,
			openWalletErr:   test.openWalletErr,
		}
		r := &RPCServer{core: tc}
		payload := handleNewWallet(r, test.params)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleOpenWallet(t *testing.T) {
	pw := encode.PassBytes("password123")
	params := &RawParams{
		PWArgs: []encode.PassBytes{pw},
		Args: []string{
			"42",
		},
	}
	tests := []struct {
		name          string
		params        *RawParams
		openWalletErr error
		wantErrCode   int
	}{{
		name:        "ok",
		params:      params,
		wantErrCode: -1,
	}, {
		name:          "core.OpenWallet error",
		params:        params,
		openWalletErr: errors.New("error"),
		wantErrCode:   msgjson.RPCOpenWalletError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{openWalletErr: test.openWalletErr}
		r := &RPCServer{core: tc}
		payload := handleOpenWallet(r, test.params)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleCloseWallet(t *testing.T) {
	tests := []struct {
		name           string
		params         *RawParams
		closeWalletErr error
		wantErrCode    int
	}{{
		name:        "ok",
		params:      &RawParams{Args: []string{"42"}},
		wantErrCode: -1,
	}, {
		name:           "core.closeWallet error",
		params:         &RawParams{Args: []string{"42"}},
		closeWalletErr: errors.New("error"),
		wantErrCode:    msgjson.RPCCloseWalletError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{closeWalletErr: test.closeWalletErr}
		r := &RPCServer{core: tc}
		payload := handleCloseWallet(r, test.params)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleWallets(t *testing.T) {
	tc := new(TCore)
	r := &RPCServer{core: tc}
	payload := handleWallets(r, nil)
	res := ""
	if err := verifyResponse(payload, &res, -1); err != nil {
		t.Fatal(err)
	}
}

func TestHandleRegister(t *testing.T) {
	pw := encode.PassBytes("password123")
	params := &RawParams{
		PWArgs: []encode.PassBytes{pw},
		Args: []string{
			"dex:1234",
			"1000",
			"cert",
		},
	}
	tests := []struct {
		name                        string
		params                      *RawParams
		preRegisterFee              uint64
		preRegisterErr, registerErr error
		wantErrCode                 int
	}{{
		name:           "ok",
		params:         params,
		preRegisterFee: 1000,
		wantErrCode:    -1,
	}, {
		name:           "preRegister fee different",
		params:         params,
		preRegisterFee: 100,
		wantErrCode:    msgjson.RPCRegisterError,
	}, {
		name:           "core.Register error",
		params:         params,
		preRegisterFee: 1000,
		registerErr:    errors.New("error"),
		wantErrCode:    msgjson.RPCRegisterError,
	}, {
		name:           "core.PreRegister error",
		params:         params,
		preRegisterErr: errors.New("error"),
		wantErrCode:    msgjson.RPCPreRegisterError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			registerErr:    test.registerErr,
			preRegisterFee: test.preRegisterFee,
			preRegisterErr: test.preRegisterErr,
		}
		r := &RPCServer{core: tc}
		payload := handleRegister(r, test.params)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}
