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

//
// From testutil_test.go
//

func init() {
	asset.Register(tUTXOAssetA.ID, &tDriver{
		decodedCoinID: tUTXOAssetA.Symbol,
		winfo:         tWalletInfo,
	}, true)
}

// makeMsg is a test helper that creates a *msgjson.Message from a route and
// typed params. For nil params (no-params handlers), it creates a message
// with nil payload.
func makeMsg(t *testing.T, route string, params any) *msgjson.Message {
	t.Helper()
	if params == nil {
		msg, err := msgjson.NewRequest(1, route, nil)
		if err != nil {
			t.Fatalf("NewRequest error: %v", err)
		}
		return msg
	}
	msg, err := msgjson.NewRequest(1, route, params)
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	return msg
}

// makeBadMsg creates a *msgjson.Message with an invalid payload that will
// fail to unmarshal into any typed params struct.
func makeBadMsg(t *testing.T, route string) *msgjson.Message {
	t.Helper()
	msg, err := msgjson.NewRequest(1, route, "bad")
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	return msg
}

func verifyResponse(payload *msgjson.ResponsePayload, res any, wantErrCode int) error {
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

var (
	wsServer    = websocket.New(&TCore{}, dex.StdOutLogger("TEST", dex.LevelTrace))
	tUTXOAssetA = &dex.Asset{
		ID:         42,
		Symbol:     "dcr",
		Version:    0, // match the stubbed (*TXCWallet).Info result
		MaxFeeRate: 10,
		SwapConf:   1,
	}
	tWalletInfo = &asset.WalletInfo{
		SupportedVersions: []uint32{0},
		UnitInfo: dex.UnitInfo{
			Conventional: dex.Denomination{
				ConversionFactor: 1e8,
			},
		},
		AvailableWallets: []*asset.WalletDefinition{{
			Type: "rpc",
		}},
	}
)

type tDriver struct {
	wallet        asset.Wallet
	decodedCoinID string
	winfo         *asset.WalletInfo
}

func (drv *tDriver) Open(cfg *asset.WalletConfig, logger dex.Logger, net dex.Network) (asset.Wallet, error) {
	return drv.wallet, nil
}

func (drv *tDriver) DecodeCoinID(coinID []byte) (string, error) {
	return drv.decodedCoinID, nil
}

func (drv *tDriver) Info() *asset.WalletInfo {
	return drv.winfo
}

type Dummy struct {
	Status string
}

// tCoin satisfies the asset.Coin interface.
type tCoin struct{}

func (tCoin) ID() dex.Bytes {
	return nil
}
func (tCoin) String() string {
	return ""
}
func (tCoin) TxID() string {
	return ""
}
func (tCoin) Value() uint64 {
	return 0
}
func (tCoin) Confirmations(context.Context) (uint32, error) {
	return 0, nil
}

//
// From routes_test.go
//

func TestCreateResponse(t *testing.T) {
	tests := []struct {
		name        string
		res         any
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

func TestRouteInfos(t *testing.T) {
	// routes and routeInfos must have the same keys.
	if len(routes) != len(routeInfos) {
		t.Fatalf("routes has %d entries but routeInfos has %d", len(routes), len(routeInfos))
	}
	for k := range routes {
		if _, exists := routeInfos[k]; !exists {
			t.Fatalf("%v exists in routes but not in routeInfos", k)
		}
	}
}

func TestListCommands(t *testing.T) {
	// Without passwords.
	res := ListCommands(false, true)
	if res == "" {
		t.Fatal("ListCommands returned empty string")
	}
	// Verify password fields are absent.
	if strings.Contains(res, "appPass") {
		t.Fatal("ListCommands(false) should not contain appPass")
	}

	// With passwords.
	resWithPW := ListCommands(true, true)
	if resWithPW == "" {
		t.Fatal("ListCommands(true) returned empty string")
	}
	// Verify password fields appear.
	if !strings.Contains(resWithPW, "appPass") {
		t.Fatal("ListCommands(true) should contain appPass")
	}

	// Without dev flag, dev routes should be hidden.
	resNoDev := ListCommands(false, false)
	if strings.Contains(resNoDev, "deploycontract") {
		t.Fatal("ListCommands with dev=false should not contain deploycontract")
	}
}

func TestCommandUsage(t *testing.T) {
	// Verify each route produces non-empty output.
	for r := range routeInfos {
		res, err := commandUsage(r, false)
		if err != nil {
			t.Fatalf("unexpected error for command %s: %v", r, err)
		}
		if res == "" {
			t.Fatalf("commandUsage returned empty string for %s", r)
		}
		// Must contain the summary.
		if !strings.Contains(res, routeInfos[r].summary) {
			t.Fatalf("commandUsage for %s does not contain summary", r)
		}
	}

	// Verify unknown command returns errUnknownCmd.
	if _, err := commandUsage("never make this command", false); !errors.Is(err, errUnknownCmd) {
		t.Fatal("expected error for bogus command")
	}

	// Verify password toggle works for a route that has passwords.
	resNoPW, _ := commandUsage("trade", false)
	resWithPW, _ := commandUsage("trade", true)
	if strings.Contains(resNoPW, "appPass") {
		t.Fatal("commandUsage(trade, false) should not contain appPass")
	}
	if !strings.Contains(resWithPW, "appPass") {
		t.Fatal("commandUsage(trade, true) should contain appPass")
	}
}

func TestFieldDescsMatchParams(t *testing.T) {
	// For every routeInfo with a paramsType, verify every key in fieldDescs
	// maps to an actual JSON field in the reflected struct.
	for route, info := range routeInfos {
		if info.paramsType == nil {
			continue
		}
		fields := reflectFields(info.paramsType, info.fieldDescs)
		jsonNames := make(map[string]bool, len(fields))
		for _, f := range fields {
			jsonNames[f.jsonName] = true
		}
		for key := range info.fieldDescs {
			if !jsonNames[key] {
				t.Errorf("route %q: fieldDescs key %q does not match any JSON field in %v",
					route, key, info.paramsType)
			}
		}
	}
}

func TestFieldDescsComplete(t *testing.T) {
	// For every routeInfo with a paramsType, verify every non-password JSON
	// field has a corresponding key in fieldDescs.
	for route, info := range routeInfos {
		if info.paramsType == nil {
			continue
		}
		fields := reflectFields(info.paramsType, info.fieldDescs)
		for _, f := range fields {
			if f.isPassword {
				continue
			}
			if _, exists := info.fieldDescs[f.jsonName]; !exists {
				t.Errorf("route %q: JSON field %q has no entry in fieldDescs", route, f.jsonName)
			}
		}
	}
}

func TestPasswordFieldDetection(t *testing.T) {
	// TradeParams should have appPass as a password field.
	fields := reflectFields(reflect.TypeOf(TradeParams{}), nil)
	foundAppPass := false
	for _, f := range fields {
		if f.jsonName == "appPass" {
			foundAppPass = true
			if !f.isPassword {
				t.Fatal("appPass in TradeParams should be detected as password")
			}
		}
	}
	if !foundAppPass {
		t.Fatal("appPass not found in TradeParams fields")
	}

	// CancelParams should have no password fields.
	fields = reflectFields(reflect.TypeOf(CancelParams{}), nil)
	for _, f := range fields {
		if f.isPassword {
			t.Fatalf("CancelParams should have no password fields, found %s", f.jsonName)
		}
	}
}

func TestEmbeddedStructFlattening(t *testing.T) {
	// TradeParams embeds core.TradeForm. Verify that fields from TradeForm
	// appear in the reflected output.
	fields := reflectFields(reflect.TypeOf(TradeParams{}), nil)
	names := make(map[string]bool, len(fields))
	for _, f := range fields {
		names[f.jsonName] = true
	}
	// "host" comes from embedded core.TradeForm
	for _, expected := range []string{"appPass", "host", "isLimit", "sell", "base", "quote", "qty", "rate", "tifnow"} {
		if !names[expected] {
			t.Errorf("expected field %q from embedded TradeForm not found in TradeParams reflection", expected)
		}
	}
}

//
// From handlers_system_test.go
//

func TestHandleHelp(t *testing.T) {
	tests := []struct {
		name        string
		params      any
		wantErrCode int
	}{{
		name:        "ok no arg",
		params:      &HelpParams{},
		wantErrCode: -1,
	}, {
		name:        "ok with arg",
		params:      &HelpParams{HelpWith: "version"},
		wantErrCode: -1,
	}, {
		name:        "unknown route",
		params:      &HelpParams{HelpWith: "versio"},
		wantErrCode: msgjson.RPCUnknownRoute,
	}, {
		name:        "bad params",
		params:      nil, // will use makeBadMsg
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, helpRoute)
		} else {
			msg = makeMsg(t, helpRoute, test.params)
		}
		payload := handleHelp(&RPCServer{}, msg)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleVersion(t *testing.T) {
	tc := &TCore{}
	r := &RPCServer{core: tc, bwVersion: &SemVersion{}}
	payload := handleVersion(r, nil)
	res := &VersionResponse{}
	if err := verifyResponse(payload, &res, -1); err != nil {
		t.Fatal(err)
	}
}

func TestHandleInit(t *testing.T) {
	pw := encode.PassBytes("password123")
	tests := []struct {
		name                string
		params              any
		initializeClientErr error
		wantErrCode         int
	}{{
		name:        "ok",
		params:      &InitParams{AppPass: pw},
		wantErrCode: -1,
	}, {
		name:                "core.InitializeClient error",
		params:              &InitParams{AppPass: pw},
		initializeClientErr: errors.New("error"),
		wantErrCode:         msgjson.RPCInitError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{initializeClientErr: test.initializeClientErr}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, initRoute)
		} else {
			msg = makeMsg(t, initRoute, test.params)
		}
		payload := handleInit(r, msg)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleLogin(t *testing.T) {
	pw := encode.PassBytes("abc")
	tests := []struct {
		name        string
		params      any
		loginErr    error
		wantErrCode int
	}{{
		name:        "ok",
		params:      &LoginParams{AppPass: pw},
		wantErrCode: -1,
	}, {
		name:        "core.Login error",
		params:      &LoginParams{AppPass: pw},
		loginErr:    errors.New("error"),
		wantErrCode: msgjson.RPCLoginError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			loginErr: test.loginErr,
		}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, loginRoute)
		} else {
			msg = makeMsg(t, loginRoute, test.params)
		}
		payload := handleLogin(r, msg)
		successString := "successfully logged in"
		if err := verifyResponse(payload, &successString, test.wantErrCode); err != nil {
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

//
// From handlers_wallet_test.go
//

func TestHandleNewWallet(t *testing.T) {
	pw := encode.PassBytes("password123")
	goodParams := &NewWalletParams{
		AppPass:    pw,
		WalletPass: pw,
		AssetID:    42,
		WalletType: "rpc",
		Config: map[string]string{
			"username": "tacotime",
			"field":    "value",
		},
	}
	badWalletDefParams := &NewWalletParams{
		AppPass:    pw,
		WalletPass: pw,
		AssetID:    45,
		WalletType: "rpc",
		Config: map[string]string{
			"username": "tacotime",
			"field":    "value",
		},
	}
	tests := []struct {
		name            string
		params          any
		walletState     *core.WalletState
		createWalletErr error
		openWalletErr   error
		wantErrCode     int
	}{{
		name:        "ok new wallet",
		params:      goodParams,
		wantErrCode: -1,
	}, {
		name:        "ok existing wallet",
		params:      goodParams,
		walletState: &core.WalletState{Open: false},
		wantErrCode: msgjson.RPCWalletExistsError,
	}, {
		name:            "core.CreateWallet error",
		params:          goodParams,
		createWalletErr: errors.New("error"),
		wantErrCode:     msgjson.RPCCreateWalletError,
	}, {
		name:          "core.OpenWallet error",
		params:        goodParams,
		openWalletErr: errors.New("error"),
		wantErrCode:   msgjson.RPCOpenWalletError,
	}, {
		name:        "bad config opts error, unknown coin",
		params:      badWalletDefParams,
		wantErrCode: msgjson.RPCWalletDefinitionError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			walletState:     test.walletState,
			createWalletErr: test.createWalletErr,
			openWalletErr:   test.openWalletErr,
		}
		r := &RPCServer{core: tc, wsServer: wsServer}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, newWalletRoute)
		} else {
			msg = makeMsg(t, newWalletRoute, test.params)
		}
		payload := handleNewWallet(r, msg)

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
	goodParams := &OpenWalletParams{
		AppPass: pw,
		AssetID: 42,
	}
	tests := []struct {
		name          string
		params        any
		openWalletErr error
		wantErrCode   int
	}{{
		name:        "ok",
		params:      goodParams,
		wantErrCode: -1,
	}, {
		name:          "core.OpenWallet error",
		params:        goodParams,
		openWalletErr: errors.New("error"),
		wantErrCode:   msgjson.RPCOpenWalletError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{openWalletErr: test.openWalletErr}
		r := &RPCServer{core: tc, wsServer: wsServer}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, openWalletRoute)
		} else {
			msg = makeMsg(t, openWalletRoute, test.params)
		}
		payload := handleOpenWallet(r, msg)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleCloseWallet(t *testing.T) {
	tests := []struct {
		name           string
		params         any
		closeWalletErr error
		wantErrCode    int
	}{{
		name:        "ok",
		params:      &CloseWalletParams{AssetID: 42},
		wantErrCode: -1,
	}, {
		name:           "core.closeWallet error",
		params:         &CloseWalletParams{AssetID: 42},
		closeWalletErr: errors.New("error"),
		wantErrCode:    msgjson.RPCCloseWalletError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{closeWalletErr: test.closeWalletErr}
		r := &RPCServer{core: tc, wsServer: wsServer}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, closeWalletRoute)
		} else {
			msg = makeMsg(t, closeWalletRoute, test.params)
		}
		payload := handleCloseWallet(r, msg)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleToggleWalletStatus(t *testing.T) {
	tests := []struct {
		name            string
		params          any
		walletStatusErr error
		wantErrCode     int
	}{{
		name:        "ok: disable",
		params:      &ToggleWalletStatusParams{AssetID: 42, Disable: true},
		wantErrCode: -1,
	}, {
		name:        "ok: enable",
		params:      &ToggleWalletStatusParams{AssetID: 42, Disable: false},
		wantErrCode: -1,
	}, {
		name:            "core.toggleWalletStatus error",
		params:          &ToggleWalletStatusParams{AssetID: 42, Disable: true},
		walletStatusErr: errors.New("error"),
		wantErrCode:     msgjson.RPCToggleWalletStatusError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{walletStatusErr: test.walletStatusErr, walletState: &core.WalletState{}}
		r := &RPCServer{core: tc, wsServer: wsServer}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, toggleWalletStatusRoute)
		} else {
			msg = makeMsg(t, toggleWalletStatusRoute, test.params)
		}
		payload := handleToggleWalletStatus(r, msg)
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

//
// From handlers_trading_test.go
//

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

func TestHandleTrade(t *testing.T) {
	pw := encode.PassBytes("abc")
	goodParams := &TradeParams{
		AppPass: pw,
		TradeForm: core.TradeForm{
			Host:    "1.2.3.4:3000",
			IsLimit: true,
			Sell:    true,
			Base:    0,
			Quote:   42,
			Qty:     1,
			Rate:    1,
			TifNow:  true,
			Options: map[string]string{"gas_price": "23", "gas_limit": "120000"},
		},
	}
	tests := []struct {
		name        string
		params      any
		tradeErr    error
		wantErrCode int
	}{{
		name:        "ok",
		params:      goodParams,
		wantErrCode: -1,
	}, {
		name:        "core.Trade error",
		params:      goodParams,
		tradeErr:    errors.New("error"),
		wantErrCode: msgjson.RPCTradeError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{order: new(core.Order), tradeErr: test.tradeErr}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, tradeRoute)
		} else {
			msg = makeMsg(t, tradeRoute, test.params)
		}
		payload := handleTrade(r, msg)
		res := new(tradeResponse)
		if err := verifyResponse(payload, res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleCancel(t *testing.T) {
	goodParams := &CancelParams{
		OrderID: "fb94fe99e4e32200a341f0f1cb33f34a08ac23eedab636e8adb991fa76343e1e",
	}
	tests := []struct {
		name        string
		params      any
		cancelErr   error
		wantErrCode int
	}{{
		name:        "ok",
		params:      goodParams,
		wantErrCode: -1,
	}, {
		name:        "core.Cancel error",
		params:      goodParams,
		cancelErr:   errors.New("error"),
		wantErrCode: msgjson.RPCCancelError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{cancelErr: test.cancelErr}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, cancelRoute)
		} else {
			msg = makeMsg(t, cancelRoute, test.params)
		}
		payload := handleCancel(r, msg)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleOrderBook(t *testing.T) {
	goodParams := &OrderBookParams{Host: "dex", Base: 42, Quote: 0}
	paramsNOrders := &OrderBookParams{Host: "dex", Base: 42, Quote: 0, NOrders: 1}
	tests := []struct {
		name        string
		params      any
		book        *core.OrderBook
		bookErr     error
		wantErrCode int
	}{{
		name:        "ok no nOrders",
		params:      goodParams,
		book:        new(core.OrderBook),
		wantErrCode: -1,
	}, {
		name:        "ok with nOrders",
		params:      paramsNOrders,
		book:        new(core.OrderBook),
		wantErrCode: -1,
	}, {
		name:        "core.Book error",
		params:      goodParams,
		bookErr:     errors.New("error"),
		wantErrCode: msgjson.RPCOrderBookError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			book:    test.book,
			bookErr: test.bookErr,
		}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, orderBookRoute)
		} else {
			msg = makeMsg(t, orderBookRoute, test.params)
		}
		payload := handleOrderBook(r, msg)
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
	base42 := uint32(42)
	quote0 := uint32(0)
	tests := []struct {
		name        string
		params      any
		wantErrCode int
	}{{
		name:        "ok no params",
		params:      &MyOrdersParams{},
		wantErrCode: -1,
	}, {
		name:        "ok with host param",
		params:      &MyOrdersParams{Host: "127.0.0.1:7232"},
		wantErrCode: -1,
	}, {
		name:        "ok with host and baseID/quoteID params",
		params:      &MyOrdersParams{Host: "127.0.0.1:7232", Base: &base42, Quote: &quote0},
		wantErrCode: -1,
	}, {
		name:        "ok with no host and baseID/quoteID params",
		params:      &MyOrdersParams{Host: "", Base: &base42, Quote: &quote0},
		wantErrCode: -1,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{exchanges: exchangesIn}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, myOrdersRoute)
		} else {
			msg = makeMsg(t, myOrdersRoute, test.params)
		}
		payload := handleMyOrders(r, msg)
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

//
// From handlers_tx_test.go
//

func TestHandleSendAndWithdraw(t *testing.T) {
	pw := encode.PassBytes("password123")
	goodParams := &SendParams{
		AppPass: pw,
		AssetID: 42,
		Value:   1000,
		Address: "abc",
	}

	tests := []struct {
		name        string
		params      any
		walletState *core.WalletState
		coin        asset.Coin
		sendErr     error
		wantErrCode int
	}{{
		name:        "ok",
		params:      goodParams,
		walletState: &core.WalletState{},
		coin:        tCoin{},
		wantErrCode: -1,
	}, {
		name:        "Send error",
		params:      goodParams,
		walletState: &core.WalletState{},
		coin:        tCoin{},
		sendErr:     errors.New("error"),
		wantErrCode: msgjson.RPCFundTransferError,
	}, {
		name: "empty password",
		params: &SendParams{
			AssetID: 42,
			Value:   1000,
			Address: "abc",
		},
		wantErrCode: msgjson.RPCFundTransferError,
	}, {
		name:        "bad params",
		params:      nil,
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
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, withdrawRoute)
		} else {
			msg = makeMsg(t, withdrawRoute, test.params)
		}
		payload := handleWithdraw(r, msg)
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
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, sendRoute)
		} else {
			msg = makeMsg(t, sendRoute, test.params)
		}
		payload := handleSend(r, msg)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleAppSeed(t *testing.T) {
	pw := encode.PassBytes("abc")
	goodParams := &AppSeedParams{AppPass: pw}
	tests := []struct {
		name          string
		params        any
		exportSeedErr error
		wantErrCode   int
	}{{
		name:        "ok",
		params:      goodParams,
		wantErrCode: -1,
	}, {
		name:          "core.ExportSeed error",
		params:        goodParams,
		exportSeedErr: errors.New("error"),
		wantErrCode:   msgjson.RPCExportSeedError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{exportSeed: "seed words here", exportSeedErr: test.exportSeedErr}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, appSeedRoute)
		} else {
			msg = makeMsg(t, appSeedRoute, test.params)
		}
		payload := handleAppSeed(r, msg)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
		res = strings.ToLower(res)
		if test.wantErrCode == -1 && res != "seed words here" {
			t.Fatalf("expected ff but got %v", res)
		}

	}
}

func TestDeleteRecords(t *testing.T) {
	olderThan := int64(123)
	goodParams := &DeleteRecordsParams{OlderThanMs: &olderThan}
	tests := []struct {
		name                     string
		params                   any
		deleteArchivedRecordsErr error
		wantErrCode              int
	}{{
		name:        "ok",
		params:      goodParams,
		wantErrCode: -1,
	}, {
		name:                     "delete archived records error",
		params:                   goodParams,
		deleteArchivedRecordsErr: errors.New(""),
		wantErrCode:              msgjson.RPCDeleteArchivedRecordsError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			deleteArchivedRecordsErr: test.deleteArchivedRecordsErr,
		}
		tc.archivedRecords = 10
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, deleteArchivedRecordsRoute)
		} else {
			msg = makeMsg(t, deleteArchivedRecordsRoute, test.params)
		}
		payload := handleDeleteArchivedRecords(r, msg)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
		if test.wantErrCode < 0 && res == "" {
			t.Fatal("expected a non empty response for success")
		}
	}
}

//
// From handlers_dex_test.go
//

func TestHandleGetDEXConfig(t *testing.T) {
	tests := []struct {
		name            string
		params          any
		getDEXConfigErr error
		wantErrCode     int
	}{{
		name:        "ok",
		params:      &GetDEXConfigParams{Host: "dex", Cert: "cert bytes"},
		wantErrCode: -1,
	}, {
		name:            "get dex conf error",
		params:          &GetDEXConfigParams{Host: "dex", Cert: "cert bytes"},
		getDEXConfigErr: errors.New(""),
		wantErrCode:     msgjson.RPCGetDEXConfigError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			getDEXConfigErr: test.getDEXConfigErr,
		}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, getDEXConfRoute)
		} else {
			msg = makeMsg(t, getDEXConfRoute, test.params)
		}
		payload := handleGetDEXConfig(r, msg)
		res := new(*core.Exchange)
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleDiscoverAcct(t *testing.T) {
	pw := encode.PassBytes("password123")
	goodParams := &DiscoverAcctParams{
		AppPass: pw,
		Addr:    "dex:1234",
		Cert:    "cert",
	}
	tests := []struct {
		name            string
		params          any
		discoverAcctErr error
		wantErrCode     int
	}{{
		name:        "ok",
		params:      goodParams,
		wantErrCode: -1,
	}, {
		name:            "discover account error",
		params:          goodParams,
		discoverAcctErr: errors.New(""),
		wantErrCode:     msgjson.RPCDiscoverAcctError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{
			discoverAcctErr: test.discoverAcctErr,
		}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, discoverAcctRoute)
		} else {
			msg = makeMsg(t, discoverAcctRoute, test.params)
		}
		payload := handleDiscoverAcct(r, msg)
		res := new(bool)
		if err := verifyResponse(payload, res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

//
// From handlers_staking_test.go
//

func TestHandleSetVSP(t *testing.T) {
	goodParams := &SetVSPParams{
		AssetID: 42,
		Addr:    "url.com",
	}
	tests := []struct {
		name        string
		params      any
		setVSPErr   error
		wantErrCode int
	}{{
		name:        "ok",
		params:      goodParams,
		wantErrCode: -1,
	}, {
		name:        "core.SetVSP error",
		params:      goodParams,
		setVSPErr:   errors.New("error"),
		wantErrCode: msgjson.RPCSetVSPError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{setVSPErr: test.setVSPErr}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, setVSPRoute)
		} else {
			msg = makeMsg(t, setVSPRoute, test.params)
		}
		payload := handleSetVSP(r, msg)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPurchaseTickets(t *testing.T) {
	pw := encode.PassBytes("password123")
	goodParams := &PurchaseTicketsParams{
		AppPass: pw,
		AssetID: 42,
		N:       2,
	}
	tc := &TCore{}
	r := &RPCServer{core: tc}
	msg := makeMsg(t, purchaseTicketsRoute, goodParams)
	payload := handlePurchaseTickets(r, msg)
	var res bool
	err := verifyResponse(payload, &res, -1)
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyResponse(payload, &res, -1); err != nil {
		t.Fatal(err)
	}
	if res != true {
		t.Fatal("result is false")
	}

	tc.purchaseTicketsErr = errors.New("test error")
	msg = makeMsg(t, purchaseTicketsRoute, goodParams)
	payload = handlePurchaseTickets(r, msg)
	if err = verifyResponse(payload, &res, msgjson.RPCPurchaseTicketsError); err != nil {
		t.Fatal(err)
	}
}

func TestHandleStakeStatus(t *testing.T) {
	goodParams := &StakeStatusParams{AssetID: 42}
	tests := []struct {
		name           string
		params         any
		stakeStatusErr error
		wantErrCode    int
	}{{
		name:        "ok",
		params:      goodParams,
		wantErrCode: -1,
	}, {
		name:           "core.StakeStatus error",
		params:         goodParams,
		stakeStatusErr: errors.New("error"),
		wantErrCode:    msgjson.RPCStakeStatusError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{stakeStatusErr: test.stakeStatusErr}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, stakeStatusRoute)
		} else {
			msg = makeMsg(t, stakeStatusRoute, test.params)
		}
		payload := handleStakeStatus(r, msg)
		res := new(asset.TicketStakingStatus)
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleSetVotingPreferences(t *testing.T) {
	goodParams := &SetVotingPreferencesParams{AssetID: 42}
	tests := []struct {
		name             string
		params           any
		setVotingPrefErr error
		wantErrCode      int
	}{{
		name:        "ok",
		params:      goodParams,
		wantErrCode: -1,
	}, {
		name:             "core.SetVotingPreferences error",
		params:           goodParams,
		setVotingPrefErr: errors.New("error"),
		wantErrCode:      msgjson.RPCSetVotingPreferencesError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{setVotingPrefErr: test.setVotingPrefErr}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, setVotingPreferencesRoute)
		} else {
			msg = makeMsg(t, setVotingPreferencesRoute, test.params)
		}
		payload := handleSetVotingPreferences(r, msg)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleWalletBalance(t *testing.T) {
	goodParams := &WalletBalanceParams{AssetID: 42}
	tests := []struct {
		name          string
		params        any
		walletBalance *core.WalletBalance
		balanceErr    error
		wantErrCode   int
	}{{
		name:          "ok",
		params:        goodParams,
		walletBalance: &core.WalletBalance{},
		wantErrCode:   -1,
	}, {
		name:        "core.AssetBalance error",
		params:      goodParams,
		balanceErr:  errors.New("error"),
		wantErrCode: msgjson.RPCInternal,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{walletBalance: test.walletBalance, balanceErr: test.balanceErr}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, walletBalanceRoute)
		} else {
			msg = makeMsg(t, walletBalanceRoute, test.params)
		}
		payload := handleWalletBalance(r, msg)
		res := &core.WalletBalance{}
		if err := verifyResponse(payload, res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHandleWalletState(t *testing.T) {
	goodParams := &WalletStateParams{AssetID: 42}
	tests := []struct {
		name        string
		params      any
		walletState *core.WalletState
		wantErrCode int
	}{{
		name:        "ok",
		params:      goodParams,
		walletState: &core.WalletState{Symbol: "dcr", AssetID: 42},
		wantErrCode: -1,
	}, {
		name:        "wallet not found",
		params:      goodParams,
		walletState: nil,
		wantErrCode: msgjson.RPCWalletNotFoundError,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}}
	for _, test := range tests {
		tc := &TCore{walletState: test.walletState}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, walletStateRoute)
		} else {
			msg = makeMsg(t, walletStateRoute, test.params)
		}
		payload := handleWalletState(r, msg)
		res := &core.WalletState{}
		if err := verifyResponse(payload, res, test.wantErrCode); err != nil {
			t.Fatal(err)
		}
	}
}

//
// Exported accessor tests
//

func TestRouteExists(t *testing.T) {
	if !RouteExists("help") {
		t.Fatal("RouteExists returned false for 'help'")
	}
	if !RouteExists("version") {
		t.Fatal("RouteExists returned false for 'version'")
	}
	if !RouteExists("trade") {
		t.Fatal("RouteExists returned false for 'trade'")
	}
	if RouteExists("nonexistentroute") {
		t.Fatal("RouteExists returned true for nonexistent route")
	}
}

func TestParamType(t *testing.T) {
	// Routes with params should return a non-nil type.
	pt := ParamType("trade")
	if pt == nil {
		t.Fatal("ParamType returned nil for 'trade'")
	}
	if pt != reflect.TypeOf(TradeParams{}) {
		t.Fatalf("ParamType('trade') = %v, want %v", pt, reflect.TypeOf(TradeParams{}))
	}

	// version has no params.
	if ParamType("version") != nil {
		t.Fatal("ParamType should return nil for 'version' (no params)")
	}

	// Nonexistent route.
	if ParamType("nonexistentroute") != nil {
		t.Fatal("ParamType should return nil for nonexistent route")
	}
}

func TestExportedReflectFields(t *testing.T) {
	pt := ParamType("trade")
	if pt == nil {
		t.Fatal("no param type for trade")
	}
	fields := ReflectFields(pt)
	if len(fields) == 0 {
		t.Fatal("ReflectFields returned no fields for TradeParams")
	}

	// Check that the exported fields have the expected metadata.
	nameSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		nameSet[f.JSONName] = true
		if f.GoType == nil {
			t.Fatalf("GoType is nil for field %s", f.JSONName)
		}
	}

	// TradeParams should have appPass (password) and host (from embedded TradeForm).
	if !nameSet["appPass"] {
		t.Fatal("appPass not found in exported ReflectFields")
	}
	if !nameSet["host"] {
		t.Fatal("host not found in exported ReflectFields")
	}

	// Verify password detection.
	for _, f := range fields {
		if f.JSONName == "appPass" && !f.IsPassword {
			t.Fatal("appPass should be marked as password")
		}
		if f.JSONName == "host" && f.IsPassword {
			t.Fatal("host should not be marked as password")
		}
	}
}

func TestHandleMMReport(t *testing.T) {
	outFile := t.TempDir() + "/report.json"
	goodParams := &MMReportParams{
		Host:       "somedex.tld:7232",
		BaseID:     42,
		QuoteID:    0,
		StartEpoch: 100,
		EndEpoch:   200,
		OutFile:    outFile,
	}
	missingOutFile := &MMReportParams{
		Host:    "somedex.tld:7232",
		BaseID:  42,
		QuoteID: 0,
	}
	tests := []struct {
		name        string
		params      any
		snaps       []*msgjson.MMEpochSnapshot
		coreErr     error
		wantErrCode int
	}{{
		name:   "ok",
		params: goodParams,
		snaps: []*msgjson.MMEpochSnapshot{{
			MarketID: "dcr_btc",
			Base:     42,
			Quote:    0,
			EpochIdx: 100,
		}},
		wantErrCode: -1,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}, {
		name:        "missing outFile",
		params:      missingOutFile,
		wantErrCode: msgjson.RPCParseError,
	}, {
		name:        "core error",
		params:      goodParams,
		coreErr:     errors.New("test error"),
		wantErrCode: msgjson.RPCInternal,
	}}
	for _, test := range tests {
		tc := &TCore{
			exportMMSnapshots:    test.snaps,
			exportMMSnapshotsErr: test.coreErr,
		}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, mmReportRoute)
		} else {
			msg = makeMsg(t, mmReportRoute, test.params)
		}
		payload := handleMMReport(r, msg)
		res := ""
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
}

func TestHandlePruneMMSnapshots(t *testing.T) {
	goodParams := &PruneMMSnapshotsParams{
		Host:        "somedex.tld:7232",
		BaseID:      42,
		QuoteID:     0,
		MinEpochIdx: 200,
	}
	zeroMinEpoch := &PruneMMSnapshotsParams{
		Host:        "somedex.tld:7232",
		BaseID:      42,
		QuoteID:     0,
		MinEpochIdx: 0,
	}
	tests := []struct {
		name        string
		params      any
		pruned      int
		coreErr     error
		wantErrCode int
	}{{
		name:        "ok",
		params:      goodParams,
		pruned:      5,
		wantErrCode: -1,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}, {
		name:        "zero minEpochIdx",
		params:      zeroMinEpoch,
		wantErrCode: msgjson.RPCParseError,
	}, {
		name:        "core error",
		params:      goodParams,
		coreErr:     errors.New("test error"),
		wantErrCode: msgjson.RPCInternal,
	}}
	for _, test := range tests {
		tc := &TCore{
			pruneMMSnapshotsResult: test.pruned,
			pruneMMSnapshotsErr:    test.coreErr,
		}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, pruneMMSnapshotsRoute)
		} else {
			msg = makeMsg(t, pruneMMSnapshotsRoute, test.params)
		}
		payload := handlePruneMMSnapshots(r, msg)
		var res int
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
}

func TestHandleDeployContract(t *testing.T) {
	bytecode := "6080604052"
	contractVer := uint32(0)
	goodResults := []*core.DeployContractResult{{
		AssetID:      60,
		Symbol:       "eth",
		ContractAddr: "0x1234",
		TxID:         "0xabcd",
	}}
	tests := []struct {
		name        string
		params      any
		results     []*core.DeployContractResult
		coreErr     error
		wantErrCode int
	}{{
		name: "ok with bytecode",
		params: &DeployContractParams{
			AppPass:  encode.PassBytes("abc"),
			Chains:   []string{"eth"},
			Bytecode: &bytecode,
		},
		results:     goodResults,
		wantErrCode: -1,
	}, {
		name: "ok with contractVer",
		params: &DeployContractParams{
			AppPass:     encode.PassBytes("abc"),
			Chains:      []string{"eth"},
			ContractVer: &contractVer,
		},
		results:     goodResults,
		wantErrCode: -1,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}, {
		name: "unknown chain",
		params: &DeployContractParams{
			AppPass:  encode.PassBytes("abc"),
			Chains:   []string{"unknownchain"},
			Bytecode: &bytecode,
		},
		wantErrCode: msgjson.RPCArgumentsError,
	}, {
		name: "missing bytecode and contractVer",
		params: &DeployContractParams{
			AppPass: encode.PassBytes("abc"),
			Chains:  []string{"eth"},
		},
		wantErrCode: msgjson.RPCArgumentsError,
	}, {
		name: "bytecode takes priority over contractVer",
		params: &DeployContractParams{
			AppPass:     encode.PassBytes("abc"),
			Chains:      []string{"eth"},
			Bytecode:    &bytecode,
			ContractVer: &contractVer,
		},
		results:     goodResults,
		wantErrCode: -1,
	}, {
		name: "core error",
		params: &DeployContractParams{
			AppPass:  encode.PassBytes("abc"),
			Chains:   []string{"eth"},
			Bytecode: &bytecode,
		},
		coreErr:     errors.New("test error"),
		wantErrCode: msgjson.RPCDeployContractError,
	}}
	for _, test := range tests {
		tc := &TCore{
			deployContractResults: test.results,
			deployContractErr:     test.coreErr,
		}
		r := &RPCServer{core: tc, dev: true}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, deployContractRoute)
		} else {
			msg = makeMsg(t, deployContractRoute, test.params)
		}
		payload := handleDeployContract(r, msg)
		var res []*core.DeployContractResult
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
}

func TestHandleTestContractGas(t *testing.T) {
	maxSwaps := 3
	goodResults := []*core.ContractGasTestResult{{
		AssetID:    60,
		Symbol:     "eth",
		Swap:       63441,
		SwapAdd:    34703,
		Redeem:     52041,
		RawSwaps:   []uint64{48801, 75511, 102209},
		RawRedeems: []uint64{40032, 50996, 61949},
		RawRefunds: []uint64{40390, 40401, 40388},
		Summary:    "test summary",
	}}
	tests := []struct {
		name        string
		params      any
		results     []*core.ContractGasTestResult
		coreErr     error
		wantErrCode int
	}{{
		name: "ok",
		params: &TestContractGasParams{
			AppPass:  encode.PassBytes("abc"),
			Chains:   []string{"eth"},
			MaxSwaps: &maxSwaps,
		},
		results:     goodResults,
		wantErrCode: -1,
	}, {
		name: "ok with tokens",
		params: &TestContractGasParams{
			AppPass: encode.PassBytes("abc"),
			Chains:  []string{"eth"},
			Tokens:  []string{"usdc.eth"},
		},
		results:     goodResults,
		wantErrCode: -1,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}, {
		name: "unknown chain",
		params: &TestContractGasParams{
			AppPass: encode.PassBytes("abc"),
			Chains:  []string{"unknownchain"},
		},
		wantErrCode: msgjson.RPCArgumentsError,
	}, {
		name: "no chains",
		params: &TestContractGasParams{
			AppPass: encode.PassBytes("abc"),
			Chains:  []string{},
		},
		wantErrCode: msgjson.RPCArgumentsError,
	}, {
		name: "unknown token",
		params: &TestContractGasParams{
			AppPass: encode.PassBytes("abc"),
			Chains:  []string{"eth"},
			Tokens:  []string{"unknown.token"},
		},
		wantErrCode: msgjson.RPCArgumentsError,
	}, {
		name: "core error",
		params: &TestContractGasParams{
			AppPass: encode.PassBytes("abc"),
			Chains:  []string{"eth"},
		},
		coreErr:     errors.New("test error"),
		wantErrCode: msgjson.RPCTestContractGasError,
	}}
	for _, test := range tests {
		tc := &TCore{
			testContractGasResults: test.results,
			testContractGasErr:     test.coreErr,
		}
		r := &RPCServer{core: tc, dev: true}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, testContractGasRoute)
		} else {
			msg = makeMsg(t, testContractGasRoute, test.params)
		}
		payload := handleTestContractGas(r, msg)
		var res []*core.ContractGasTestResult
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
}

func TestHandleReconfigureWallet(t *testing.T) {
	tests := []struct {
		name        string
		params      any
		coreErr     error
		wantErrCode int
	}{{
		name: "ok",
		params: &ReconfigureWalletParams{
			AppPass:    encode.PassBytes("abc"),
			AssetID:    42,
			WalletType: "rpc",
			Config:     map[string]string{"key": "val"},
		},
		wantErrCode: -1,
	}, {
		name: "ok with new password",
		params: &ReconfigureWalletParams{
			AppPass:     encode.PassBytes("abc"),
			NewWalletPW: encode.PassBytes("newpw"),
			AssetID:     42,
			WalletType:  "rpc",
			Config:      map[string]string{"key": "val"},
		},
		wantErrCode: -1,
	}, {
		name:        "bad params",
		params:      nil,
		wantErrCode: msgjson.RPCArgumentsError,
	}, {
		name: "core error",
		params: &ReconfigureWalletParams{
			AppPass:    encode.PassBytes("abc"),
			AssetID:    42,
			WalletType: "rpc",
			Config:     map[string]string{"key": "val"},
		},
		coreErr:     errors.New("test error"),
		wantErrCode: msgjson.RPCReconfigureWalletError,
	}}
	for _, test := range tests {
		tc := &TCore{
			reconfigureWalletErr: test.coreErr,
		}
		r := &RPCServer{core: tc}
		var msg *msgjson.Message
		if test.params == nil {
			msg = makeBadMsg(t, reconfigureWalletRoute)
		} else {
			msg = makeMsg(t, reconfigureWalletRoute, test.params)
		}
		payload := handleReconfigureWallet(r, msg)
		var res string
		if err := verifyResponse(payload, &res, test.wantErrCode); err != nil {
			t.Fatalf("%s: %v", test.name, err)
		}
	}
}
