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
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"github.com/decred/slog"
)

func init() {
	log = slog.NewBackend(os.Stdout).Logger("TEST")
	log.SetLevel(slog.LevelTrace)
}

var (
	errT    = fmt.Errorf("test error")
	tLogger dex.Logger
	tCtx    context.Context
)

type TCore struct {
	preRegisterFee      uint64
	preRegisterErr      error
	balanceErr          error
	syncBook            *core.OrderBook
	syncFeed            *core.BookFeed
	syncErr             error
	createWalletErr     error
	openWalletErr       error
	walletState         *core.WalletState
	closeWalletErr      error
	wallets             []*core.WalletState
	initializeClientErr error
	registerErr         error
}

func (c *TCore) Book(dex string, base, quote uint32) (*core.OrderBook, error) {
	return nil, nil
}
func (c *TCore) Balance(uint32) (uint64, error) {
	return 0, c.balanceErr
}
func (c *TCore) CreateWallet(appPW, walletPW encode.PassBytes, form *core.WalletForm) error {
	return c.createWalletErr
}
func (c *TCore) CloseWallet(assetID uint32) error {
	return c.closeWalletErr
}
func (c *TCore) InitializeClient(pw encode.PassBytes) error {
	return c.initializeClientErr
}
func (c *TCore) OpenWallet(assetID uint32, pw encode.PassBytes) error {
	return c.openWalletErr
}
func (c *TCore) PreRegister(*core.PreRegisterForm) (uint64, error) {
	return c.preRegisterFee, c.preRegisterErr
}
func (c *TCore) Register(*core.RegisterForm) error {
	return c.registerErr
}
func (c *TCore) Sync(dex string, base, quote uint32) (*core.OrderBook, *core.BookFeed, error) {
	return nil, core.NewBookFeed(func(*core.BookFeed) {}), c.syncErr
}
func (c *TCore) Unsync(dex string, base, quote uint32) {}
func (c *TCore) Wallets() []*core.WalletState {
	return c.wallets
}
func (c *TCore) WalletState(assetID uint32) *core.WalletState {
	return c.walletState
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

type TConn struct {
	msg       []byte
	reads     [][]byte      // data for ReadMessage
	respReady chan []byte   // signal from WriteMessage
	close     chan struct{} // Close tells ReadMessage to return with error
}

// ReadMessage must not return constantly with nothing
var readTimeout = 10 * time.Second

func (c *TConn) ReadMessage() (int, []byte, error) {
	if len(c.reads) > 0 {
		var read []byte
		// pop front
		read, c.reads = c.reads[0], c.reads[1:]
		return len(read), read, nil
	}

	select {
	case <-c.close: // receive from nil channel blocks
		return 0, nil, fmt.Errorf("closed")
	case <-time.After(readTimeout):
		return 0, nil, fmt.Errorf("read timeout")
	}
}

func (c *TConn) addRead(read []byte) {
	// push back
	c.reads = append(c.reads, read)
}

func (c *TConn) WriteMessage(_ int, msg []byte) error {
	c.msg = msg
	select {
	case c.respReady <- msg:
	default:
	}
	return nil
}

func (c *TConn) SetWriteDeadline(_ time.Time) error {
	return nil
}

func (c *TConn) WriteControl(messageType int, data []byte, deadline time.Time) error {
	return nil
}

func (c *TConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *TConn) Close() error {
	// If the test has a non-nil close channel, signal close.
	select {
	case c.close <- struct{}{}:
	default:
	}
	return nil
}

type tLink struct {
	cl   *wsClient
	conn *TConn
}

func newLink() *tLink {
	conn := &TConn{
		respReady: make(chan []byte, 1),
	}
	cl := newWSClient("", conn,
		func(*msgjson.Message) *msgjson.Error { return nil })
	return &tLink{
		cl:   cl,
		conn: conn,
	}
}

var tPort = 5555

func newTServer(t *testing.T, start bool, user, pass string) (*RPCServer,
	*TCore, func()) {
	c := &TCore{}
	var shutdown func()
	ctx, killCtx := context.WithCancel(tCtx)
	tmp, err := os.Getwd()
	if err != nil {
		t.Error(err)
	}
	cert, key := tmp+"/cert.cert", tmp+"/key.key"
	defer os.Remove(cert)
	defer os.Remove(key)
	cfg := &Config{c, fmt.Sprintf("localhost:%d", tPort), user, pass, cert,
		key}
	s, err := New(cfg)
	if err != nil {
		t.Fatalf("error creating server: %v", err)
	}
	if start {
		waiter := dex.NewStartStopWaiter(s)
		waiter.Start(ctx)
		shutdown = func() {
			killCtx()
			waiter.WaitForShutdown()
		}
	} else {
		shutdown = killCtx
		s.ctx = ctx
	}
	return s, c, shutdown
}

func ensureResponse(t *testing.T, s *RPCServer, f func(w http.ResponseWriter,
	r *http.Request), want string, reader *TReader, writer *TWriter,
	body interface{}) {
	reader.msg, _ = json.Marshal(body)
	req, err := http.NewRequest("POST", "/", reader)
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
	tLogger = slog.NewBackend(os.Stdout).Logger("TEST")
	tLogger.SetLevel(slog.LevelTrace)
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	doIt := func() int {
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestLoadMarket(t *testing.T) {
	link := newLink()
	s, tCore, shutdown := newTServer(t, false, "", "")
	defer shutdown()
	link.cl.Start()
	defer link.cl.Disconnect()
	params := &marketLoad{
		DEX:   "abc",
		Base:  uint32(1),
		Quote: uint32(2),
	}

	subscription, _ := msgjson.NewRequest(1, "loadmarket", params)
	tCore.syncBook = &core.OrderBook{}

	extractMessage := func() *msgjson.Message {
		select {
		case msgB := <-link.conn.respReady:
			msg := new(msgjson.Message)
			json.Unmarshal(msgB, &msg)
			return msg
		case <-time.NewTimer(time.Millisecond * 100).C:
			t.Fatalf("extractMessage got nothing")
		}
		return nil
	}

	ensureGood := func() {
		// Create a new feed for every request because a Close()d feed cannot be
		// reused.
		tCore.syncFeed = core.NewBookFeed(func(feed *core.BookFeed) {})
		msgErr := s.handleMessage(link.cl, subscription)
		if msgErr != nil {
			t.Fatalf("'loadmarket' error: %d: %s", msgErr.Code, msgErr.Message)
		}
		msg := extractMessage()
		if msg.Route != "book" {
			t.Fatalf("wrong message received. Expected 'book', got %s", msg.Route)
		}
		if link.cl.feedLoop == nil {
			t.Fatalf("nil book feed waiter after 'loadmarket'")
		}
	}

	// Initial success.
	ensureGood()

	// Unsubscribe.
	unsub, _ := msgjson.NewRequest(2, "unmarket", nil)
	msgErr := s.handleMessage(link.cl, unsub)
	if msgErr != nil {
		t.Fatalf("'unmarket' error: %d: %s", msgErr.Code, msgErr.Message)
	}

	if link.cl.feedLoop != nil {
		t.Fatalf("non-nil book feed waiter after 'unmarket'")
	}

	// Make sure a sync error propagates.
	tCore.syncErr = fmt.Errorf("test error")
	msgErr = s.handleMessage(link.cl, subscription)
	if msgErr == nil {
		t.Fatalf("no handleMessage error from Sync error")
	}
	tCore.syncErr = nil

	// Success again.
	ensureGood()
}

func TestHandleMessage(t *testing.T) {
	link := newLink()
	s, _, shutdown := newTServer(t, false, "", "")
	defer shutdown()
	var msg *msgjson.Message

	ensureErr := func(name string, wantCode int) {
		got := s.handleMessage(link.cl, msg)
		if got == nil {
			t.Fatalf("%s: no error", name)
		}
		if wantCode != got.Code {
			t.Fatalf("%s, wanted %d, got %d",
				name, wantCode, got.Code)
		}
	}

	// Send a response, which is unsupported on the server.
	msg, _ = msgjson.NewResponse(1, nil, nil)
	ensureErr("bad route", msgjson.UnknownMessageType)

	// Unknown route.
	msg, _ = msgjson.NewRequest(1, "123", nil)
	ensureErr("bad route", msgjson.RPCUnknownRoute)

	// Set the route correctly.
	wsHandlers["123"] = func(*RPCServer, *wsClient, *msgjson.Message) *msgjson.Error {
		return nil
	}

	rpcErr := s.handleMessage(link.cl, msg)
	if rpcErr != nil {
		t.Fatalf("error for good message: %d: %s",
			rpcErr.Code, rpcErr.Message)
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
	s, _, shutdown := newTServer(t, false, "", "")
	defer shutdown()
	var r *http.Request

	ensureHTTPError := func(name string, wantCode int) {
		w := &tResponseWriter{}
		s.handleJSON(w, r)
		if w.code != wantCode {
			t.Fatalf("Expected HTTP error %d, got %d",
				wantCode, w.code)
		}
	}

	ensureMsgErr := func(name string, wantCode int) {
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
		s, _, shutdown := newTServer(t, false, test[0], test[1])
		auth := base64.StdEncoding.EncodeToString((s.authsha[:]))
		if auth != test[2] {
			t.Fatalf("expected auth %s but got %s", test[2], auth)
		}
		shutdown()
	}
}

func TestAuthMiddleware(t *testing.T) {
	s, _, shutdown := newTServer(t, false, "", "")
	defer shutdown()
	am := s.authMiddleware(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
	r, _ := http.NewRequest("GET", "", nil)

	wantAuthError := func(name string, want bool) {
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
	s.authsha = sha256.Sum256([]byte(auth))

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

func TestClientMap(t *testing.T) {
	s, _, shutdown := newTServer(t, true, "", "")
	resp := make(chan []byte, 1)
	conn := &TConn{
		respReady: resp,
		close:     make(chan struct{}, 1),
	}
	// msg.ID == 0 gets an error response, which can be discarded.
	read, _ := json.Marshal(msgjson.Message{ID: 0})
	conn.addRead(read)

	go s.websocketHandler(conn, "someip")

	// When a response to our dummy message is received, the client should
	// be in RPCServer's client map.
	<-resp

	// While we're here, check that the client is properly mapped.
	var cl *wsClient
	s.mtx.Lock()
	i := len(s.clients)
	if i != 1 {
		t.Fatalf("expected 1 client in server map, found %d", i)
	}
	for _, c := range s.clients {
		cl = c
		break
	}
	s.mtx.Unlock()

	// Close the server and make sure the connection is closed.
	shutdown()
	if !cl.Off() {
		t.Fatalf("connection not closed on server shutdown")
	}
}
