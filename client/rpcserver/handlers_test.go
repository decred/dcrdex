// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/websocket"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"github.com/davecgh/go-spew/spew"
)

func verifyResponse(payload *msgjson.ResponsePayload, res interface{}, wantErrCode int) error {
	if wantErrCode != -1 {
		if payload.Error == nil {
			return errors.New("no error")
		}
		if payload.Error.Code != wantErrCode {
			return errors.New("wrong error code")
		}
	} else {
		if payload.Error != nil {
			return fmt.Errorf("unexpected error: %v", payload.Error)
		}
	}
	if err := json.Unmarshal(payload.Result, res); err != nil {
		return fmt.Errorf("unable to unmarshal res: %v", err)
	}
	return nil
}

var wsServer = websocket.New(&TCore{}, dex.StdOutLogger("TEST", dex.LevelTrace))

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
	tc := &TCore{}
	r := &RPCServer{core: tc, dexcVersion: &SemVersion{}}
	payload := handleVersion(r, nil)
	res := &VersionResponse{}
	if err := verifyResponse(payload, &res, -1); err != nil {
		t.Fatal(err)
	}
}

func TestHandleGetDEXConfig(t *testing.T) {
	tests := []struct {
		name            string
		params          *RawParams
		getDEXConfigErr error
		wantErrCode     int
	}{{
		name:        "ok",
		params:      &RawParams{Args: []string{"dex", "cert bytes"}},
		wantErrCode: -1,
	}, {
		name:            "get dex conf error",
		params:          &RawParams{Args: []string{"dex", "cert bytes"}},
		getDEXConfigErr: errors.New(""),
		wantErrCode:     msgjson.RPCGetDEXConfigError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			getDEXConfigErr: test.getDEXConfigErr,
		}
		r := &RPCServer{core: tc}
		payload := handleGetDEXConfig(r, test.params)
		res := new(*core.Exchange)
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
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
			"rpc",
			"username=tacotime",
			`{"field":"value"}`,
		},
	}
	badJSONParams := &RawParams{
		PWArgs: []encode.PassBytes{pw, pw},
		Args: []string{
			"42",
			"rpc",
			"username=tacotime",
			`{"field":  value"}`,
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
		name:        "bad JSON error",
		params:      badJSONParams,
		wantErrCode: msgjson.RPCArgumentsError,
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
		r := &RPCServer{core: tc, wsServer: wsServer}
		payload := handleNewWallet(r, test.params)

		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
		if test.wantErrCode == -1 {
			if tc.newWalletForm.AssetID != 42 {
				t.Fatalf("assetID not parsed correctly")
			}
			cfg := tc.newWalletForm.Config
			if cfg["username"] != "tacotime" {
				t.Fatalf("file config not parsed correctly")
			}
			if cfg["field"] != "value" {
				t.Fatalf("custom config not parsed correctly")
			}
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
		r := &RPCServer{core: tc, wsServer: wsServer}
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
		r := &RPCServer{core: tc, wsServer: wsServer}
		payload := handleCloseWallet(r, test.params)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleToggleWalletStatus(t *testing.T) {
	tests := []struct {
		name            string
		params          *RawParams
		walletStatusErr error
		wantErrCode     int
	}{{
		name:        "ok: disable",
		params:      &RawParams{Args: []string{"42", "true"}},
		wantErrCode: -1,
	}, {
		name:        "ok: enable",
		params:      &RawParams{Args: []string{"42", "false"}},
		wantErrCode: -1,
	}, {
		name:            "core.toggleWalletStatus error",
		params:          &RawParams{Args: []string{"42", "true"}},
		walletStatusErr: errors.New("error"),
		wantErrCode:     msgjson.RPCToggleWalletStatusError,
	}, {
		name:        "bad params",
		params:      &RawParams{Args: []string{"42"}},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{walletStatusErr: test.walletStatusErr, walletState: &core.WalletState{}}
		r := &RPCServer{core: tc, wsServer: wsServer}
		payload := handleToggleWalletStatus(r, test.params)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatalf("%s failed: %v", test.name, err)
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
			"42",
			"cert",
		},
	}
	dcrFeeAsset := &core.FeeAsset{Amt: 1000, ID: 42}
	tests := []struct {
		name                         string
		params                       *RawParams
		regFees                      map[string]*core.FeeAsset
		getDEXConfigErr, registerErr error
		wantErrCode                  int
	}{{
		name:        "ok",
		params:      params,
		regFees:     map[string]*core.FeeAsset{"dcr": dcrFeeAsset},
		wantErrCode: -1,
	}, {
		name:        "fee different",
		params:      params,
		regFees:     map[string]*core.FeeAsset{"dcr": {Amt: 100, ID: 42}},
		wantErrCode: msgjson.RPCRegisterError,
	}, {
		name:        "core.Register error",
		params:      params,
		regFees:     map[string]*core.FeeAsset{"dcr": dcrFeeAsset},
		registerErr: errors.New("error"),
		wantErrCode: msgjson.RPCRegisterError,
	}, {
		name:            "core.GetDEXConfig error",
		params:          params,
		getDEXConfigErr: errors.New("error"),
		wantErrCode:     msgjson.RPCGetDEXConfigError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			registerErr: test.registerErr,
			dexExchange: &core.Exchange{
				RegFees: test.regFees,
			},
			getDEXConfigErr: test.getDEXConfigErr,
		}
		r := &RPCServer{core: tc}
		payload := handleRegister(r, test.params)
		res := new(core.RegisterResult)
		if err := verifyResponse(payload, res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

const exchangeIn = `{
  "https://127.0.0.1:7232": {
    "host": "https://127.0.0.1:7232",
    "acctID": "edc7620e02",
    "markets": {
      "dcr_btc": {
        "name": "dcr_btc",
        "baseid": 42,
        "basesymbol": "dcr",
        "quoteid": 0,
        "quotesymbol": "btc",
        "epochlen": 10000,
        "startepoch": 158891349,
        "buybuffer": 1.25,
        "orders": [
          {
            "dex": "https://127.0.0.1:7232",
            "market": "dcr_btc",
            "type": 1,
            "id": "e016a563ff5b845e9af20718af72224af630e65ca53edf2a3342d175dc6d3738",
            "stamp": 1588913556583,
            "qty": 100000000,
            "sell": false,
            "sig": "3045022100c5ef66cbf3c2d305408b666108ae384478f22b558893942b8f66abfb613a5bf802205eb22a0250e5286244b2f5205f0b6d6b4fa6a60930be2ff30f35c3cf6bf969c8",
            "filled": 0,
            "matches": [
              {
                "matchID": "1472deb169fb359a48676161be8ca81983201f28abe8cc9b504950032d6f14ec",
                "qty": 100000000,
                "rate": 100000000,
                "step": 1
              }
            ],
            "cancelling": false,
            "canceled": false,
            "rate": 100000000,
            "tif": 1
          }
        ]
      }
    },
    "assets": {
      "0": {
        "id": 0,
        "symbol": "btc",
        "lotSize": 100000,
        "rateStep": 100000,
        "maxFeeRate": 100,
        "swapSize": 225,
        "swapSizeBase": 76,
        "swapConf": 1
      },
      "42": {
        "id": 42,
        "symbol": "dcr",
        "lotSize": 100000000,
        "rateStep": 100000000,
        "maxFeeRate": 10,
        "swapSize": 251,
        "swapSizeBase": 85,
        "swapConf": 1
      }
    },
    "regFees": {
      "dcr": {
        "id": 42,
        "amt": 2000000,
        "confs": 1
	  }
	}
  }
}`

const exchangeOut = `{
  "https://127.0.0.1:7232": {
    "acctID": "edc7620e02",
    "markets": {
      "dcr_btc": {
        "baseid": 42,
        "basesymbol": "dcr",
        "quoteid": 0,
        "quotesymbol": "btc",
        "epochlen": 10000,
        "startepoch": 158891349,
        "buybuffer": 1.25
      }
    },
    "assets": {
      "0": {
        "symbol": "btc",
        "lotSize": 100000,
        "rateStep": 100000,
        "maxFeeRate": 100,
        "swapSize": 225,
        "swapSizeBase": 76,
        "swapConf": 1
      },
      "42": {
        "symbol": "dcr",
        "lotSize": 100000000,
        "rateStep": 100000000,
        "maxFeeRate": 10,
        "swapSize": 251,
        "swapSizeBase": 85,
        "swapConf": 1
      }
    },
    "regFees": {
      "dcr": {
        "id": 42,
        "amt": 2000000,
        "confs": 1
      }
    }
  }
}`

func TestHandleExchanges(t *testing.T) {
	var exchangesIn map[string]*core.Exchange
	if err := json.Unmarshal([]byte(exchangeIn), &exchangesIn); err != nil {
		panic(err)
	}
	tc := &TCore{exchanges: exchangesIn}
	r := &RPCServer{core: tc}
	payload := handleExchanges(r, nil)
	var res map[string]*core.Exchange
	if err := verifyResponse(payload, &res, -1); err != nil {
		t.Fatal(err)
	}
	var exchangesOut map[string]*core.Exchange
	if err := json.Unmarshal([]byte(exchangeOut), &exchangesOut); err != nil {
		panic(err)
	}
	if !reflect.DeepEqual(res, exchangesOut) {
		t.Fatalf("expected %v but got %v", spew.Sdump(exchangesOut), spew.Sdump(res))
	}
}

func TestHandleLogin(t *testing.T) {
	params := &RawParams{PWArgs: []encode.PassBytes{encode.PassBytes("abc")}}
	tests := []struct {
		name        string
		params      *RawParams
		loginErr    error
		wantErrCode int
	}{{
		name:        "ok",
		params:      params,
		wantErrCode: -1,
	}, {
		name:        "core.Login error",
		params:      params,
		loginErr:    errors.New("error"),
		wantErrCode: msgjson.RPCLoginError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			loginErr: test.loginErr,
		}
		r := &RPCServer{core: tc}
		payload := handleLogin(r, test.params)
		successString := "successfully logged in"
		if err := verifyResponse(payload, &successString, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleTrade(t *testing.T) {
	params := &RawParams{
		PWArgs: []encode.PassBytes{encode.PassBytes("abc")}, // 0. AppPass
		Args: []string{
			"1.2.3.4:3000", // 0. DEX
			"true",         // 1. IsLimit
			"true",         // 2. Sell
			"0",            // 3. Base
			"42",           // 4. Quote
			"1",            // 5. Qty
			"1",            // 6. Rate
			"true",         // 7. TifNow
			`{"gas_price":"23","gas_limit":"120000"}`, // 8. Options
		}}
	tests := []struct {
		name        string
		params      *RawParams
		tradeErr    error
		wantErrCode int
	}{{
		name:        "ok",
		params:      params,
		wantErrCode: -1,
	}, {
		name:        "core.Trade error",
		params:      params,
		tradeErr:    errors.New("error"),
		wantErrCode: msgjson.RPCTradeError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{order: new(core.Order), tradeErr: test.tradeErr}
		r := &RPCServer{core: tc}
		payload := handleTrade(r, test.params)
		res := new(tradeResponse)
		if err := verifyResponse(payload, res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleCancel(t *testing.T) {
	params := &RawParams{
		PWArgs: []encode.PassBytes{encode.PassBytes("abc")},
		Args:   []string{"fb94fe99e4e32200a341f0f1cb33f34a08ac23eedab636e8adb991fa76343e1e"},
	}
	tests := []struct {
		name        string
		params      *RawParams
		cancelErr   error
		wantErrCode int
	}{{
		name:        "ok",
		params:      params,
		wantErrCode: -1,
	}, {
		name:        "core.Cancel error",
		params:      params,
		cancelErr:   errors.New("error"),
		wantErrCode: msgjson.RPCCancelError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{cancelErr: test.cancelErr}
		r := &RPCServer{core: tc}
		payload := handleCancel(r, test.params)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

// tCoin satisfies the asset.Coin interface.
type tCoin struct{}

func (tCoin) ID() dex.Bytes {
	return nil
}
func (tCoin) String() string {
	return ""
}
func (tCoin) Value() uint64 {
	return 0
}
func (tCoin) Confirmations(context.Context) (uint32, error) {
	return 0, nil
}

func TestHandleSendAndWithdraw(t *testing.T) {
	pw := encode.PassBytes("password123")
	params := &RawParams{
		PWArgs: []encode.PassBytes{pw},
		Args: []string{
			"42",
			"1000",
			"abc",
		},
	}

	tests := []struct {
		name        string
		params      *RawParams
		walletState *core.WalletState
		coin        asset.Coin
		sendErr     error
		wantErrCode int
	}{{
		name:        "ok",
		params:      params,
		walletState: &core.WalletState{},
		coin:        tCoin{},
		wantErrCode: -1,
	}, {
		name:        "Send error",
		params:      params,
		walletState: &core.WalletState{},
		coin:        tCoin{},
		sendErr:     errors.New("error"),
		wantErrCode: msgjson.RPCFundTransferError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}

	// Test handleWithdraw.
	for _, test := range tests {
		tc := &TCore{
			walletState: test.walletState,
			coin:        test.coin,
			sendErr:     test.sendErr,
		}
		r := &RPCServer{core: tc}
		payload := handleWithdraw(r, test.params)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}

	// Test handleSend.
	for _, test := range tests {
		tc := &TCore{
			walletState: test.walletState,
			coin:        test.coin,
			sendErr:     test.sendErr,
		}
		r := &RPCServer{core: tc}
		payload := handleSend(r, test.params)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleLogout(t *testing.T) {
	tests := []struct {
		name        string
		logoutErr   error
		wantErrCode int
	}{{
		name:        "ok",
		wantErrCode: -1,
	}, {
		name:        "core.Logout error",
		logoutErr:   errors.New("error"),
		wantErrCode: msgjson.RPCLogoutError,
	}}
	for _, test := range tests {
		tc := &TCore{
			logoutErr: test.logoutErr,
		}
		r := &RPCServer{core: tc}
		payload := handleLogout(r, nil)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleOrderBook(t *testing.T) {
	params := &RawParams{Args: []string{"dex", "42", "0"}}
	paramsNOrders := &RawParams{Args: []string{"dex", "42", "0", "1"}}
	tests := []struct {
		name        string
		params      *RawParams
		book        *core.OrderBook
		bookErr     error
		wantErrCode int
	}{{
		name:        "ok no nOrders",
		params:      params,
		book:        new(core.OrderBook),
		wantErrCode: -1,
	}, {
		name:        "ok with nOrders",
		params:      paramsNOrders,
		book:        new(core.OrderBook),
		wantErrCode: -1,
	}, {
		name:        "core.Book error",
		params:      params,
		bookErr:     errors.New("error"),
		wantErrCode: msgjson.RPCOrderBookError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			book:    test.book,
			bookErr: test.bookErr,
		}
		r := &RPCServer{core: tc}
		payload := handleOrderBook(r, test.params)
		res := new(core.OrderBook)
		if err := verifyResponse(payload, res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTruncateOrderBook(t *testing.T) {
	var lowRate uint64 = 1e8
	var medRate uint64 = 1.5e8
	var highRate uint64 = 2.0e8
	lowRateOrder := &core.MiniOrder{MsgRate: lowRate}
	medRateOrder := &core.MiniOrder{MsgRate: medRate}
	highRateOrder := &core.MiniOrder{MsgRate: highRate}
	book := &core.OrderBook{
		Buys: []*core.MiniOrder{
			highRateOrder,
			medRateOrder,
			lowRateOrder,
		},
		Sells: []*core.MiniOrder{
			lowRateOrder,
			medRateOrder,
		},
	}
	truncateOrderBook(book, 4)
	// no change
	if len(book.Buys) != 3 && len(book.Sells) != 2 {
		t.Fatal("no change was expected")
	}
	truncateOrderBook(book, 3)
	// no change
	if len(book.Buys) != 3 && len(book.Sells) != 2 {
		t.Fatal("no change was expected")
	}
	truncateOrderBook(book, 2)
	// buys truncated
	if len(book.Buys) != 2 && len(book.Sells) != 2 {
		t.Fatal("buys not truncated")
	}
	truncateOrderBook(book, 1)
	// buys and sells truncated
	if len(book.Buys) != 1 && len(book.Sells) != 1 {
		t.Fatal("buys and sells not truncated")
	}
	if book.Buys[0].MsgRate != highRate {
		t.Fatal("expected high rate order")
	}
	if book.Sells[0].MsgRate != lowRate {
		t.Fatal("expected low rate order")
	}
}

func TestHandleMyOrders(t *testing.T) {
	var exchangesIn map[string]*core.Exchange
	if err := json.Unmarshal([]byte(exchangeIn), &exchangesIn); err != nil {
		panic(err)
	}
	paramsWithArgs := func(ss ...string) *RawParams {
		args := []string{}
		args = append(args, ss...)
		return &RawParams{Args: args}
	}
	tests := []struct {
		name        string
		params      *RawParams
		wantErrCode int
	}{{
		name:        "ok no params",
		params:      paramsWithArgs(),
		wantErrCode: -1,
	}, {
		name:        "ok with host param",
		params:      paramsWithArgs("127.0.0.1:7232"),
		wantErrCode: -1,
	}, {
		name:        "ok with host and baseID/quoteID params",
		params:      paramsWithArgs("127.0.0.1:7232", "42", "0"),
		wantErrCode: -1,
	}, {
		name:        "ok with no host and baseID/quoteID params",
		params:      paramsWithArgs("", "42", "0"),
		wantErrCode: -1,
	}, {
		name:        "bad params",
		params:      paramsWithArgs("", "42"), // missing quote ID
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{exchanges: exchangesIn}
		r := &RPCServer{core: tc}
		payload := handleMyOrders(r, test.params)
		res := new(myOrdersResponse)
		if err := verifyResponse(payload, res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParseCoreOrder(t *testing.T) {
	co := `{
    "canceled": false,
    "cancelling": false,
    "epoch": 159650082,
    "filled": 300000000,
    "host": "127.0.0.1:7232",
    "id": "ca0097c87dbf01169d76b6f2a318f88fe0ea678df3139f09d756d7d3e2c602dd",
    "market": "dcr_btc",
    "matches": [
      {
        "matchID": "992f15e89bbd670663b690b4da4a859609d83866e200f3c4cd5c916442b8ea46",
        "qty": 100000000,
        "rate": 200000000,
        "side": 0,
        "status": 4
      },
      {
        "matchID": "69d7453d8ad3b52851c2c9925499a1b158301e8a08da594428ef0ad4cd6fd3a5",
        "qty": 200000000,
        "rate": 200000000,
        "side": 0,
        "status": 1
      }
    ],
    "qty": 400000000,
    "rate": 200000000,
    "sell": false,
    "sig": "30450221008eaf7fa3e5b4374800d11e50af419d3fa7c75362dce136df98a25eccc84e61380220458132451b40aa6951ab5b61d6b55c478fd9535c3d24fd4957070c7879e465ff",
    "stamp": 1596500829705,
    "status": 2,
    "tif": 1,
    "type": 1
  }`

	mo := `{
    "host": "127.0.0.1:7232",
    "marketName": "dcr_btc",
    "baseID": 42,
    "quoteID": 0,
    "id": "ca0097c87dbf01169d76b6f2a318f88fe0ea678df3139f09d756d7d3e2c602dd",
    "type": "limit",
    "sell": false,
    "stamp": 1596500829705,
    "age": "2.664424s",
    "rate": 200000000,
    "quantity": 400000000,
    "filled": 300000000,
    "settled": 100000000,
    "status": "booked",
    "tif": "standing",
    "matches": [
      {
        "matchID": "992f15e89bbd670663b690b4da4a859609d83866e200f3c4cd5c916442b8ea46",
        "status": "MatchComplete",
        "revoked": false,
        "rate": 200000000,
        "qty": 100000000,
        "side": "Maker",
        "feeRate": 0,
        "swap": "",
        "counterSwap":"",
        "redeem": "",
        "counterRedeem": "",
        "refund": "",
        "stamp": 0,
        "isCancel": false
     	},
      {
        "matchID": "69d7453d8ad3b52851c2c9925499a1b158301e8a08da594428ef0ad4cd6fd3a5",
        "status": "MakerSwapCast",
        "revoked": false,
        "rate": 200000000,
        "qty": 200000000,
        "side": "Maker",
        "feeRate": 0,
        "swap": "",
        "counterSwap": "",
        "redeem": "",
        "counterRedeem": "",
        "refund": "",
        "tamp": 0,
        "isCancel": false
      }
    ]
  }`
	coreOrder := new(core.Order)
	if err := json.Unmarshal([]byte(co), coreOrder); err != nil {
		panic(err)
	}
	myOrder := new(myOrder)
	if err := json.Unmarshal([]byte(mo), myOrder); err != nil {
		panic(err)
	}

	res := parseCoreOrder(coreOrder, 42, 0)
	// Age will differ as it is based on the current time.
	myOrder.Age = res.Age
	if !reflect.DeepEqual(myOrder, res) {
		t.Fatalf("expected %v but got %v", spew.Sdump(myOrder), spew.Sdump(res))
	}
}

func TestHandleAppSeed(t *testing.T) {
	params := &RawParams{
		PWArgs: []encode.PassBytes{encode.PassBytes("abc")},
	}
	tests := []struct {
		name          string
		params        *RawParams
		exportSeedErr error
		wantErrCode   int
	}{{
		name:        "ok",
		params:      params,
		wantErrCode: -1,
	}, {
		name:          "core.ExportSeed error",
		params:        params,
		exportSeedErr: errors.New("error"),
		wantErrCode:   msgjson.RPCExportSeedError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{exportSeed: []byte{255}, exportSeedErr: test.exportSeedErr}
		r := &RPCServer{core: tc}
		payload := handleAppSeed(r, test.params)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
		res = strings.ToLower(res)
		if test.wantErrCode == -1 && res != "ff" {
			t.Fatalf("expected ff but got %v", res)
		}

	}
}

func TestHandleDiscoverAcct(t *testing.T) {
	pw := encode.PassBytes("password123")
	params := &RawParams{
		PWArgs: []encode.PassBytes{pw},
		Args: []string{
			"dex:1234",
			"cert",
		},
	}
	tests := []struct {
		name            string
		params          *RawParams
		discoverAcctErr error
		wantErrCode     int
	}{{
		name:        "ok",
		params:      params,
		wantErrCode: -1,
	}, {
		name:            "discover account error",
		params:          params,
		discoverAcctErr: errors.New(""),
		wantErrCode:     msgjson.RPCDiscoverAcctError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			discoverAcctErr: test.discoverAcctErr,
		}
		r := &RPCServer{core: tc}
		payload := handleDiscoverAcct(r, test.params)
		res := new(bool)
		if err := verifyResponse(payload, res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDeleteRecords(t *testing.T) {
	params := &RawParams{
		Args: []string{
			"123",
		},
	}
	tests := []struct {
		name                     string
		params                   *RawParams
		deleteArchivedRecordsErr error
		wantErrCode              int
	}{{
		name:        "ok",
		params:      params,
		wantErrCode: -1,
	}, {
		name:                     "delete archived records error",
		params:                   params,
		deleteArchivedRecordsErr: errors.New(""),
		wantErrCode:              msgjson.RPCDeleteArchivedRecordsError,
	}, {
		name: "bad params",
		params: &RawParams{
			Args: []string{
				"abc",
			},
		},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			deleteArchivedRecordsErr: test.deleteArchivedRecordsErr,
		}
		tc.archivedRecords = 10
		r := &RPCServer{core: tc}
		payload := handleDeleteArchivedRecords(r, test.params)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
		if test.wantErrCode < 0 && res == "" {
			t.Fatal("expected a non empty response for success")
		}
	}
}
