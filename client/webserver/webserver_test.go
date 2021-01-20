// +build !live

package webserver

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"github.com/go-chi/chi"
)

var (
	tErr    = fmt.Errorf("expected dummy error")
	tLogger dex.Logger
	tCtx    context.Context
)

type tCoin struct {
	id       []byte
	confs    uint32
	confsErr error
}

func (c *tCoin) ID() dex.Bytes {
	return c.id
}

func (c *tCoin) String() string {
	return hex.EncodeToString(c.id)
}

func (c *tCoin) Value() uint64 {
	return 0
}

func (c *tCoin) Confirmations(context.Context) (uint32, error) {
	return c.confs, c.confsErr
}

type TCore struct {
	balanceErr      error
	syncFeed        *core.BookFeed
	syncErr         error
	regErr          error
	loginErr        error
	logoutErr       error
	initErr         error
	getFeeErr       error
	createWalletErr error
	openWalletErr   error
	closeWalletErr  error
	withdrawErr     error
	notHas          bool
	notRunning      bool
	notOpen         bool
}

func (c *TCore) Network() dex.Network                                        { return dex.Mainnet }
func (c *TCore) Exchanges() map[string]*core.Exchange                        { return nil }
func (c *TCore) GetFee(string, interface{}) (uint64, error)                  { return 1e8, c.getFeeErr }
func (c *TCore) Register(r *core.RegisterForm) (*core.RegisterResult, error) { return nil, c.regErr }
func (c *TCore) InitializeClient(pw []byte) error                            { return c.initErr }
func (c *TCore) Login(pw []byte) (*core.LoginResult, error)                  { return &core.LoginResult{}, c.loginErr }
func (c *TCore) SyncBook(dex string, base, quote uint32) (*core.BookFeed, error) {
	return c.syncFeed, c.syncErr
}
func (c *TCore) Book(dex string, base, quote uint32) (*core.OrderBook, error) {
	return &core.OrderBook{}, nil
}
func (c *TCore) AssetBalance(assetID uint32) (*core.WalletBalance, error) { return nil, c.balanceErr }
func (c *TCore) WalletState(assetID uint32) *core.WalletState {
	if c.notHas {
		return nil
	}
	return &core.WalletState{
		Symbol:  unbip(assetID),
		AssetID: assetID,
		Open:    !c.notOpen,
		Running: !c.notRunning,
	}
}
func (c *TCore) CreateWallet(appPW, walletPW []byte, form *core.WalletForm) error {
	return c.createWalletErr
}
func (c *TCore) OpenWallet(assetID uint32, pw []byte) error       { return c.openWalletErr }
func (c *TCore) CloseWallet(assetID uint32) error                 { return c.closeWalletErr }
func (c *TCore) ConnectWallet(assetID uint32) error               { return nil }
func (c *TCore) Wallets() []*core.WalletState                     { return nil }
func (c *TCore) WalletSettings(uint32) (map[string]string, error) { return nil, nil }
func (c *TCore) ReconfigureWallet(aPW, nPW []byte, assetID uint32, cfg map[string]string) error {
	return nil
}
func (c *TCore) SetWalletPassword(appPW []byte, assetID uint32, newPW []byte) error { return nil }
func (c *TCore) NewDepositAddress(assetID uint32) (string, error)                   { return "", nil }
func (c *TCore) AutoWalletConfig(assetID uint32) (map[string]string, error)         { return nil, nil }
func (c *TCore) User() *core.User                                                   { return nil }
func (c *TCore) SupportedAssets() map[uint32]*core.SupportedAsset {
	return make(map[uint32]*core.SupportedAsset)
}
func (c *TCore) Withdraw(pw []byte, assetID uint32, value uint64, address string) (asset.Coin, error) {
	return &tCoin{id: []byte{0xde, 0xc7, 0xed}}, c.withdrawErr
}
func (c *TCore) Trade(pw []byte, form *core.TradeForm) (*core.Order, error) {
	oType := order.LimitOrderType
	if !form.IsLimit {
		oType = order.MarketOrderType
	}
	return &core.Order{
		Type:  oType,
		Stamp: encode.UnixMilliU(time.Now()),
		Rate:  form.Rate,
		Qty:   form.Qty,
		Sell:  form.Sell,
	}, nil
}

func (c *TCore) Cancel(pw []byte, oid dex.Bytes) error { return nil }

func (c *TCore) NotificationFeed() <-chan core.Notification { return make(chan core.Notification, 1) }

func (c *TCore) AckNotes(ids []dex.Bytes) {}

func (c *TCore) Logout() error { return c.logoutErr }

func (c *TCore) Orders(*core.OrderFilter) ([]*core.Order, error) { return nil, nil }
func (c *TCore) Order(oid dex.Bytes) (*core.Order, error)        { return nil, nil }
func (c *TCore) MaxBuy(host string, base, quote uint32, rate uint64) (*core.OrderEstimate, error) {
	return nil, nil
}
func (c *TCore) MaxSell(host string, base, quote uint32) (*core.OrderEstimate, error) {
	return nil, nil
}
func (c *TCore) AccountExport(pw []byte, host string) (*core.AccountResponse, error) {
	return nil, nil
}

type TWriter struct {
	b []byte
}

func (*TWriter) Header() http.Header {
	return http.Header{}
}

func (w *TWriter) Write(b []byte) (int, error) {
	w.b = b
	return len(b), nil
}

func (w *TWriter) WriteHeader(int) {}

type TReader struct {
	msg []byte
	err error
}

func (r *TReader) Read(p []byte) (n int, err error) {
	if r.err != nil {
		return 0, r.err
	}
	if len(r.msg) == 0 {
		return 0, io.EOF
	}
	copy(p, r.msg)
	if len(p) < len(r.msg) {
		r.msg = r.msg[:len(p)]
		return len(p), nil
	}
	l := len(r.msg)
	r.msg = nil
	return l, io.EOF
}

func (r *TReader) Close() error { return nil }

func newTServer(t *testing.T, start bool) (*WebServer, *TCore, func(), error) {
	t.Helper()
	c := &TCore{}
	var shutdown func()
	ctx, killCtx := context.WithCancel(tCtx)
	s, err := New(c, "127.0.0.1:0", tLogger, false)
	if err != nil {
		t.Fatalf("error creating server: %v", err)
	}

	if start {
		cm := dex.NewConnectionMaster(s)
		err := cm.Connect(ctx)
		if err != nil {
			t.Fatalf("Error starting WebServer: %v", err)
		}
		shutdown = func() {
			killCtx()
			cm.Disconnect()
		}
	} else {
		shutdown = killCtx
	}
	return s, c, shutdown, err
}

func ensureResponse(t *testing.T, f func(w http.ResponseWriter, r *http.Request), want string, reader *TReader, writer *TWriter, body interface{}) {
	t.Helper()
	var err error
	reader.msg, err = json.Marshal(body)
	if err != nil {
		t.Fatalf("error marshalling request body: %v", err)
	}
	req, err := http.NewRequest("GET", "/", reader)
	if err != nil {
		t.Fatalf("error creating request: %v", err)
	}
	f(writer, req)
	if len(writer.b) == 0 {
		t.Fatalf("no response")
	}
	// Drop the line feed.
	errMsg := string(writer.b[:len(writer.b)-1])
	if errMsg != want {
		t.Fatalf("wrong response. expected %s, got %s", want, errMsg)
	}
	writer.b = nil
}

func TestMain(m *testing.M) {
	tLogger = dex.StdOutLogger("TEST", dex.LevelTrace)
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	doIt := func() int {
		// Not counted as coverage, must test Archiver constructor explicitly.
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestNew_siteError(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("cannot get current directory: %v", err)
	}

	// Change to a directory with no "site" or "../../webserver/site" folder.
	dir, _ := ioutil.TempDir("", "test")
	defer os.RemoveAll(dir)
	defer os.Chdir(cwd) // leave the temp dir before trying to delete it

	if err = os.Chdir(dir); err != nil {
		t.Fatalf("Cannot cd to %q", dir)
	}

	c := &TCore{}
	_, err = New(c, "127.0.0.1:0", tLogger, false)
	if err == nil || !strings.HasPrefix(err.Error(), "no HTML template files found") {
		t.Errorf("Should have failed to start with no site folder.")
	}
}

func TestConnectStart(t *testing.T) {
	_, _, shutdown, err := newTServer(t, true)
	defer shutdown()

	if err != nil {
		t.Fatalf("error starting web server: %s", err)
	}
}

func TestConnectBindError(t *testing.T) {
	s0, _, shutdown, _ := newTServer(t, true)
	defer shutdown()

	tAddr := s0.addr
	s, err := New(&TCore{}, tAddr, tLogger, false)
	if err != nil {
		t.Fatalf("error creating server: %v", err)
	}

	cm := dex.NewConnectionMaster(s)
	err = cm.Connect(tCtx)
	if err == nil {
		shutdown() // shutdown both servers with shared context
		cm.Disconnect()
		t.Fatalf("should have failed to bind")
	}
}

func TestAPIRegister(t *testing.T) {
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore, shutdown, _ := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		t.Helper()
		ensureResponse(t, s.apiRegister, want, reader, writer, body)
	}

	goodBody := &core.RegisterForm{
		Addr:    "test",
		AppPass: []byte("pass"),
	}
	body = goodBody
	ensure(`{"ok":true}`)

	// Likely not going to happen, but check the read error.
	reader.err = tErr
	ensure("error reading JSON message")
	reader.err = nil

	// Send nonsense
	body = []byte("nonsense")
	ensure("failed to unmarshal JSON request")
	body = goodBody

	// Registration error
	tCore.regErr = tErr
	ensure(fmt.Sprintf(`{"ok":false,"msg":"registration error: %s"}`, tErr))
	tCore.regErr = nil
}

func TestAPILogin(t *testing.T) {
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore, shutdown, _ := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s.apiLogin, want, reader, writer, body)
	}

	goodBody := &loginForm{
		Pass: encode.PassBytes("def"),
	}
	body = goodBody
	ensure(`{"ok":true,"notes":null}`)

	// Login error
	tCore.loginErr = tErr
	ensure(fmt.Sprintf(`{"ok":false,"msg":"login error: %s"}`, tErr))
	tCore.loginErr = nil
}

func TestAPIWithdraw(t *testing.T) {
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore, shutdown, _ := newTServer(t, false)
	defer shutdown()

	isOK := func() bool {
		reader.msg, _ = json.Marshal(body)
		req, err := http.NewRequest("GET", "/", reader)
		if err != nil {
			t.Fatalf("error creating request: %v", err)
		}
		s.apiWithdraw(writer, req)
		if len(writer.b) == 0 {
			t.Fatalf("no response")
		}
		resp := &standardResponse{}
		err = json.Unmarshal(writer.b, resp)
		if err != nil {
			t.Fatalf("json unmarshal error: %v", err)
		}
		return resp.OK
	}

	body = &withdrawForm{}

	// initial success
	if !isOK() {
		t.Fatalf("not ok: %s", string(writer.b))
	}

	// no wallet
	tCore.notHas = true
	if isOK() {
		t.Fatalf("no error for missing wallet")
	}
	tCore.notHas = false

	// withdraw error
	tCore.withdrawErr = tErr
	if isOK() {
		t.Fatalf("no error for Withdraw error")
	}
	tCore.withdrawErr = nil

	// re-success
	if !isOK() {
		t.Fatalf("not ok afterwards: %s", string(writer.b))
	}
}

func TestAPIInit(t *testing.T) {
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore, shutdown, _ := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s.apiInit, want, reader, writer, body)
	}

	goodBody := &loginForm{
		Pass: encode.PassBytes("def"),
	}
	body = goodBody
	ensure(`{"ok":true,"notes":null}`)

	// Initialization error
	tCore.initErr = tErr
	ensure(fmt.Sprintf(`{"ok":false,"msg":"initialization error: %s"}`, tErr))
	tCore.initErr = nil
}

func TestAPIGetFee(t *testing.T) {
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore, shutdown, _ := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s.apiGetFee, want, reader, writer, body)
	}

	body = &registration{Addr: "somedexaddress.org"}
	ensure(`{"ok":true,"fee":100000000}`)

	// getFee error
	tCore.getFeeErr = tErr
	ensure(fmt.Sprintf(`{"ok":false,"msg":"%s"}`, tErr))
	tCore.getFeeErr = nil
}

func TestAPINewWallet(t *testing.T) {
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore, shutdown, _ := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s.apiNewWallet, want, reader, writer, body)
	}

	body = &newWalletForm{
		Pass: encode.PassBytes("abc"),
	}
	tCore.notHas = true
	ensure(`{"ok":true}`)

	tCore.notHas = false
	ensure(`{"ok":false,"msg":"already have a wallet for btc"}`)
	tCore.notHas = true

	tCore.createWalletErr = tErr
	ensure(fmt.Sprintf(`{"ok":false,"msg":"error creating btc wallet: %s"}`, tErr))
	tCore.createWalletErr = nil

	tCore.notHas = false
}

func TestAPILogout(t *testing.T) {
	writer := new(TWriter)
	reader := new(TReader)
	s, tCore, shutdown, _ := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s.apiLogout, want, reader, writer, nil)
	}
	ensure(`{"ok":true}`)

	// Logout error
	tCore.logoutErr = tErr
	ensure(fmt.Sprintf(`{"ok":false,"msg":"logout error: %s"}`, tErr))
	tCore.logoutErr = nil
}

func TestApiGetBalance(t *testing.T) {
	writer := new(TWriter)
	reader := new(TReader)
	s, tCore, shutdown, _ := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s.apiGetBalance, want, reader, writer, struct{}{})
	}
	ensure(`{"ok":true,"balance":null}`)

	// Logout error
	tCore.balanceErr = tErr
	ensure(fmt.Sprintf(`{"ok":false,"msg":"balance error: %s"}`, tErr))
	tCore.balanceErr = nil
}

type tHTTPHandler struct {
	req *http.Request
}

func (h *tHTTPHandler) ServeHTTP(_ http.ResponseWriter, req *http.Request) {
	h.req = req
}

func TestOrderIDCtx(t *testing.T) {
	hexOID := hex.EncodeToString(encode.RandomBytes(32))
	req := (&http.Request{}).WithContext(context.WithValue(context.Background(), chi.RouteCtxKey, &chi.Context{
		URLParams: chi.RouteParams{
			Keys:   []string{"oid"},
			Values: []string{hexOID},
		},
	}))

	tNextHandler := &tHTTPHandler{}
	handlerFunc := orderIDCtx(tNextHandler)
	handlerFunc.ServeHTTP(nil, req)

	reqCtx := tNextHandler.req.Context()
	untypedOID := reqCtx.Value(ctxOID)
	if untypedOID == nil {
		t.Fatalf("oid not embedded in request context")
	}
	oidStr, ok := untypedOID.(string)
	if !ok {
		t.Fatalf("string type assertion failed")
	}

	if oidStr != hexOID {
		t.Fatalf("wrong value embedded in request context. wanted %s, got %s", hexOID, oidStr)
	}
}

func TestGetOrderIDCtx(t *testing.T) {
	oid := encode.RandomBytes(32)
	hexOID := hex.EncodeToString(oid)

	r := (&http.Request{}).WithContext(context.WithValue(context.Background(), ctxOID, hexOID))

	bytesOut, err := getOrderIDCtx(r)
	if err != nil {
		t.Fatalf("getOrderIDCtx error: %v", err)
	}
	if len(bytesOut) == 0 {
		t.Fatalf("empty oid")
	}
	if !bytes.Equal(oid, bytesOut) {
		t.Fatalf("wrong bytes. wanted %x, got %s", oid, bytesOut)
	}

	// Test some negative paths
	for name, v := range map[string]interface{}{
		"nil":          nil,
		"int":          5,
		"wrong length": "abc",
		"not hex":      "zyxwzyxwzyxwzyxwzyxwzyxwzyxwzyxwzyxwzyxwzyxwzyxwzyxwzyxwzyxwzyxw",
	} {
		r := (&http.Request{}).WithContext(context.WithValue(context.Background(), ctxOID, v))
		_, err := getOrderIDCtx(r)
		if err == nil {
			t.Fatalf("no error for %v", name)
		}
	}
}
