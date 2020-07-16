// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
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

func TestHandleGetFee(t *testing.T) {
	tests := []struct {
		name        string
		params      *RawParams
		regFee      uint64
		getFeeErr   error
		wantErrCode int
	}{{
		name:        "ok",
		params:      &RawParams{Args: []string{"dex", "cert"}},
		regFee:      5,
		wantErrCode: -1,
	}, {
		name:        "core.getFee error",
		params:      &RawParams{Args: []string{"dex", "cert"}},
		getFeeErr:   errors.New("error"),
		wantErrCode: msgjson.RPCGetFeeError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			regFee:    test.regFee,
			getFeeErr: test.getFeeErr,
		}
		r := &RPCServer{core: tc}
		payload := handleGetFee(r, test.params)
		res := new(getFeeResponse)
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
		if test.wantErrCode == -1 && res.Fee != test.regFee {
			t.Fatalf("wanted registration fee %d but got %d for test %s",
				test.regFee, res.Fee, test.name)
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
			"username=tacotime",
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
		name                   string
		params                 *RawParams
		regFee                 uint64
		getFeeErr, registerErr error
		wantErrCode            int
	}{{
		name:        "ok",
		params:      params,
		regFee:      1000,
		wantErrCode: -1,
	}, {
		name:        "fee different",
		params:      params,
		regFee:      100,
		wantErrCode: msgjson.RPCRegisterError,
	}, {
		name:        "core.Register error",
		params:      params,
		regFee:      1000,
		registerErr: errors.New("error"),
		wantErrCode: msgjson.RPCRegisterError,
	}, {
		name:        "core.GetFee error",
		params:      params,
		getFeeErr:   errors.New("error"),
		wantErrCode: msgjson.RPCGetFeeError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			registerErr: test.registerErr,
			regFee:      test.regFee,
			getFeeErr:   test.getFeeErr,
		}
		r := &RPCServer{core: tc}
		payload := handleRegister(r, test.params)
		res := new(core.RegisterResult)
		if err := verifyResponse(payload, res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleExchanges(t *testing.T) {
	/*
	   handleExchanges removes some redundant fields from the response.
	   $ diff in out
	   3d2
	   <     "host": "https://127.0.0.1:7232",
	   6d4
	   <         "name": "dcr_btc",
	   16,17d13
	   <             "dex": "https://127.0.0.1:7232",
	   <             "market": "dcr_btc",
	   42d37
	   <         "id": 0,
	   52d46
	   <         "id": 42,
	*/
	in := `{
  "https://127.0.0.1:7232": {
    "host": "https://127.0.0.1:7232",
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
	"confsrequired": 1,
	"confs": null
  }
}`
	out := `{
  "https://127.0.0.1:7232": {
    "markets": {
      "dcr_btc": {
        "baseid": 42,
        "basesymbol": "dcr",
        "quoteid": 0,
        "quotesymbol": "btc",
        "epochlen": 10000,
        "startepoch": 158891349,
        "buybuffer": 1.25,
        "orders": [
          {
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
    "confsrequired": 1,
    "confs": null
  }
}`
	var exchangesIn map[string]*core.Exchange
	if err := json.Unmarshal([]byte(in), &exchangesIn); err != nil {
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
	if err := json.Unmarshal([]byte(out), &exchangesOut); err != nil {
		panic(err)
	}
	if !reflect.DeepEqual(res, exchangesOut) {
		t.Fatal("result does not have expected fields removed")
	}
}

func TestHandleLogin(t *testing.T) {
	params := &RawParams{PWArgs: []encode.PassBytes{encode.PassBytes("123")}}
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
			loginResult: &core.LoginResult{},
			loginErr:    test.loginErr,
		}
		r := &RPCServer{core: tc}
		payload := handleLogin(r, test.params)
		var res *core.LoginResult
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleTrade(t *testing.T) {
	params := &RawParams{
		PWArgs: []encode.PassBytes{encode.PassBytes("123")}, // 0. AppPass
		Args: []string{
			"1.2.3.4:3000", // 0. DEX
			"true",         // 1. IsLimit
			"true",         // 2. Sell
			"0",            // 3. Base
			"42",           // 4. Quote
			"1",            // 5. Qty
			"1",            // 6. Rate
			"true",         // 7. TifNow
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
		PWArgs: []encode.PassBytes{encode.PassBytes("123")},
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

// tCoin satifies the asset.Coin interface.
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
func (tCoin) Confirmations() (uint32, error) {
	return 0, nil
}
func (tCoin) Redeem() dex.Bytes {
	return nil
}

func TestHandleWithdraw(t *testing.T) {
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
		withdrawErr error
		wantErrCode int
	}{{
		name:        "ok",
		params:      params,
		walletState: &core.WalletState{},
		coin:        tCoin{},
		wantErrCode: -1,
	}, {
		name:        "core.Withdraw error",
		params:      params,
		walletState: &core.WalletState{},
		coin:        tCoin{},
		withdrawErr: errors.New("error"),
		wantErrCode: msgjson.RPCWithdrawError,
	}, {
		name:        "bad params",
		params:      &RawParams{},
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			walletState: test.walletState,
			coin:        test.coin,
			withdrawErr: test.withdrawErr,
		}
		r := &RPCServer{core: tc}
		payload := handleWithdraw(r, test.params)
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
	lowRate := 1.0
	medRate := 1.5
	highRate := 2.0
	lowRateOrder := &core.MiniOrder{Rate: lowRate}
	medRateOrder := &core.MiniOrder{Rate: medRate}
	highRateOrder := &core.MiniOrder{Rate: highRate}
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
	if book.Buys[0].Rate != highRate {
		t.Fatal("expected high rate order")
	}
	if book.Sells[0].Rate != lowRate {
		t.Fatal("expected low rate order")
	}
}
