// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// +build !live

package rpcserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
)

func init() {
	log = dex.StdOutLogger("TEST", dex.LevelTrace)
}

var (
	tCtx context.Context
)

type TCore struct {
	regFee              uint64
	getFeeErr           error
	balanceErr          error
	syncErr             error
	createWalletErr     error
	newWalletForm       *core.WalletForm
	openWalletErr       error
	walletState         *core.WalletState
	closeWalletErr      error
	wallets             []*core.WalletState
	initializeClientErr error
	registerResult      *core.RegisterResult
	registerErr         error
	exchanges           map[string]*core.Exchange
	loginErr            error
	loginResult         *core.LoginResult
	order               *core.Order
	tradeErr            error
	cancelErr           error
	coin                asset.Coin
	withdrawErr         error
	logoutErr           error
	book                *core.OrderBook
	bookErr             error
}

func (c *TCore) Balance(uint32) (uint64, error) {
	return 0, c.balanceErr
}
func (c *TCore) Book(dex string, base, quote uint32) (*core.OrderBook, error) {
	return c.book, c.bookErr
}
func (c *TCore) AckNotes(ids []dex.Bytes) {}
func (c *TCore) AssetBalance(uint32) (*db.Balance, error) {
	return nil, c.balanceErr
}
func (c *TCore) Cancel(pw []byte, sid string) error {
	return c.cancelErr
}
func (c *TCore) CreateWallet(appPW, walletPW []byte, form *core.WalletForm) error {
	c.newWalletForm = form
	return c.createWalletErr
}
func (c *TCore) CloseWallet(assetID uint32) error {
	return c.closeWalletErr
}
func (c *TCore) Exchanges() (exchanges map[string]*core.Exchange) { return c.exchanges }
func (c *TCore) InitializeClient(pw []byte) error {
	return c.initializeClientErr
}
func (c *TCore) Login(appPass []byte) (*core.LoginResult, error) {
	return c.loginResult, c.loginErr
}
func (c *TCore) Logout() error {
	return c.logoutErr
}
func (c *TCore) OpenWallet(assetID uint32, pw []byte) error {
	return c.openWalletErr
}
func (c *TCore) GetFee(url, cert string) (uint64, error) {
	return c.regFee, c.getFeeErr
}
func (c *TCore) Register(*core.RegisterForm) (*core.RegisterResult, error) {
	return c.registerResult, c.registerErr
}
func (c *TCore) SyncBook(dex string, base, quote uint32) (*core.OrderBook, *core.BookFeed, error) {
	return nil, core.NewBookFeed(func(*core.BookFeed) {}), c.syncErr
}
func (c *TCore) Trade(appPass []byte, form *core.TradeForm) (order *core.Order, err error) {
	return c.order, c.tradeErr
}
func (c *TCore) Wallets() []*core.WalletState {
	return c.wallets
}
func (c *TCore) WalletState(assetID uint32) *core.WalletState {
	return c.walletState
}
func (c *TCore) Withdraw(pw []byte, assetID uint32, value uint64, addr string) (asset.Coin, error) {
	return c.coin, c.withdrawErr
}

func newTServer(t *testing.T, start bool, user, pass string) (*RPCServer, func()) {
	t.Helper()

	var shutdown func()
	ctx, killCtx := context.WithCancel(tCtx)
	tempDir, err := ioutil.TempDir("", "rpcservertest")
	if err != nil {
		t.Fatalf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cert, key := tempDir+"/cert.cert", tempDir+"/key.key"
	cfg := &Config{
		Core: &TCore{},
		Addr: "127.0.0.1:0",
		User: user,
		Pass: pass,
		Cert: cert,
		Key:  key,
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("error creating server: %v", err)
	}
	if start {
		cm := dex.NewConnectionMaster(s)
		err := cm.Connect(ctx)
		if err != nil {
			t.Fatalf("Error starting RPCServer: %v", err)
		}
		shutdown = func() {
			killCtx()
			cm.Disconnect()
		}
	} else {
		shutdown = killCtx
	}
	return s, shutdown
}

func TestMain(m *testing.M) {
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	doIt := func() int {
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestConnectBindError(t *testing.T) {
	s0, shutdown := newTServer(t, true, "", "")
	defer shutdown()

	tempDir, err := ioutil.TempDir("", "rpcservertest")
	if err != nil {
		t.Fatalf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cert, key := tempDir+"/cert.cert", tempDir+"/key.key"
	cfg := &Config{
		Core: &TCore{},
		Addr: s0.addr,
		User: "",
		Pass: "",
		Cert: cert,
		Key:  key,
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("error creating server: %v", err)
	}

	cm := dex.NewConnectionMaster(s)
	if err = cm.Connect(tCtx); err == nil {
		shutdown() // shutdown both servers with shared context
		cm.Disconnect()
		t.Fatal("should have failed to bind")
	}
}

type tResponseWriter struct {
	b    []byte
	code int
}

func (w *tResponseWriter) Header() http.Header {
	return make(http.Header)
}
func (w *tResponseWriter) Write(msg []byte) (int, error) {
	w.b = msg
	return len(msg), nil
}
func (w *tResponseWriter) WriteHeader(statusCode int) {
	w.code = statusCode
}

func TestParseHTTPRequest(t *testing.T) {
	s, shutdown := newTServer(t, false, "", "")
	defer shutdown()
	var r *http.Request

	ensureHTTPError := func(name string, wantCode int) {
		t.Helper()
		w := &tResponseWriter{}
		s.handleJSON(w, r)
		if w.code != wantCode {
			t.Fatalf("%s: Expected HTTP error %d, got %d",
				name, wantCode, w.code)
		}
	}

	ensureMsgErr := func(name string, wantCode int) {
		t.Helper()
		w := &tResponseWriter{}
		s.handleJSON(w, r)
		if w.code != 200 {
			t.Fatalf("HTTP error when expecting msgjson.Error")
		}
		resp := new(msgjson.Message)
		if err := json.Unmarshal(w.b, resp); err != nil {
			t.Fatalf("unable to unmarshal response: %v", err)
		}
		payload := new(msgjson.ResponsePayload)
		if err := json.Unmarshal(resp.Payload, payload); err != nil {
			t.Fatalf("unable to unmarshal payload: %v", err)
		}
		if payload.Error == nil {
			t.Fatalf("%s: no error", name)
		}
		if wantCode != payload.Error.Code {
			t.Fatalf("%s, wanted %d, got %d",
				name, wantCode, payload.Error.Code)
		}
	}
	ensureNoErr := func(name string) {
		t.Helper()
		w := &tResponseWriter{}
		s.handleJSON(w, r)
		if w.code != 200 {
			t.Fatalf("HTTP error when expecting no error")
		}
		resp := new(msgjson.Message)
		if err := json.Unmarshal(w.b, resp); err != nil {
			t.Fatalf("unable to unmarshal response: %v", err)
		}
		payload := new(msgjson.ResponsePayload)
		if err := json.Unmarshal(resp.Payload, payload); err != nil {
			t.Fatalf("unable to unmarshal payload: %v", err)
		}
		if payload.Error != nil {
			t.Fatalf("%s: errored", name)
		}
	}

	// Send a response, which is unsupported on the server.
	msg, _ := msgjson.NewResponse(1, nil, nil)
	b, _ := json.Marshal(msg)
	bbuff := bytes.NewBuffer(b)
	r, _ = http.NewRequest("GET", "", bbuff)
	ensureHTTPError("response", http.StatusMethodNotAllowed)

	// Unknown route.
	msg, _ = msgjson.NewRequest(1, "123", nil)
	b, _ = json.Marshal(msg)
	bbuff = bytes.NewBuffer(b)
	r, _ = http.NewRequest("GET", "", bbuff)
	ensureMsgErr("bad route", msgjson.RPCUnknownRoute)

	// Use real route.
	msg, _ = msgjson.NewRequest(1, "version", nil)
	b, _ = json.Marshal(msg)
	bbuff = bytes.NewBuffer(b)
	r, _ = http.NewRequest("GET", "", bbuff)
	ensureNoErr("good request")

	// Use real route with bad args.
	msg, _ = msgjson.NewRequest(1, "version", "something")
	b, _ = json.Marshal(msg)
	bbuff = bytes.NewBuffer(b)
	r, _ = http.NewRequest("GET", "", bbuff)
	ensureMsgErr("bad params", msgjson.RPCParseError)
}

type authMiddlewareTest struct {
	name, user, pass, header string
	hasAuth, wantErr         bool
}

func TestNew(t *testing.T) {
	authTests := [][]string{
		{"user", "pass", "AK+rg3mIGeouojwZwNRMjBjZouASr4mu4FWMTXQQcD0="},
		{"", "", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
		{`&!"#$%&'()~=`, `+<>*?,:.;/][{}`,
			"Te4g4+Ke9Q07MYo3iT1OCqq5qXX2ZcB47FBiVaT41hQ="},
	}
	for _, test := range authTests {
		s, shutdown := newTServer(t, false, test[0], test[1])
		auth := base64.StdEncoding.EncodeToString((s.authSHA[:]))
		if auth != test[2] {
			t.Fatalf("expected auth %s but got %s", test[2], auth)
		}
		shutdown()
	}
}

func TestAuthMiddleware(t *testing.T) {
	s, shutdown := newTServer(t, false, "", "")
	defer shutdown()
	am := s.authMiddleware(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
	r, _ := http.NewRequest("GET", "", nil)

	wantAuthError := func(name string, want bool) {
		t.Helper()
		w := &tResponseWriter{}
		am.ServeHTTP(w, r)
		if w.code != http.StatusUnauthorized && w.code != http.StatusOK {
			t.Fatalf("unexpected HTTP error %d for test \"%s\"",
				w.code, name)
		}
		switch want {
		case true:
			if w.code != http.StatusUnauthorized {
				t.Fatalf("Expected unauthorized HTTP error for test \"%s\"",
					name)
			}
		case false:
			if w.code != http.StatusOK {
				t.Fatalf("Expected OK HTTP status for test \"%s\"",
					name)
			}
		}
	}

	user, pass := "Which one is it?", "It's the one that says bmf on it."
	login := user + ":" + pass
	h := "Basic "
	auth := h + base64.StdEncoding.EncodeToString([]byte(login))
	s.authSHA = sha256.Sum256([]byte(auth))

	tests := []authMiddlewareTest{
		{"auth ok", user, pass, h, true, false},
		{"wrong pass", user, "password123", h, true, true},
		{"unknown user", "Jules", pass, h, true, true},
		{"no header", user, pass, h, false, true},
		{"malformed header", user, pass, "basic ", true, true},
	}
	for _, test := range tests {
		login = test.user + ":" + test.pass
		auth = test.header + base64.StdEncoding.EncodeToString([]byte(login))
		requestHeader := make(http.Header)
		if test.hasAuth {
			requestHeader.Add("Authorization", auth)
		}
		r.Header = requestHeader
		wantAuthError(test.name, test.wantErr)
	}
}
