// +build !live

package webserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"github.com/decred/slog"
)

var (
	tErr    = fmt.Errorf("test error")
	tLogger dex.Logger
	tCtx    context.Context
)

type TCore struct {
	balanceErr error
	syncErr    error
	regErr     error
	loginErr   error
}

func (c *TCore) Markets() map[string][]*core.Market                  { return nil }
func (c *TCore) Register(r *core.Registration) (error, <-chan error) { return c.regErr, nil }
func (c *TCore) Login(dex, pw string) error                          { return c.loginErr }
func (c *TCore) Sync(dex string, base, quote uint32) (chan *core.BookUpdate, error) {
	return nil, c.syncErr
}
func (c *TCore) Book(dex string, base, quote uint32) *core.OrderBook   { return nil }
func (c *TCore) Unsync(dex string, base, quote uint32)                 {}
func (c *TCore) Balance(uint32) (uint64, error)                        { return 0, c.balanceErr }
func (c *TCore) WalletStatus(assetID uint32) (has, running, open bool) { return true, true, true }
func (c *TCore) CreateWallet(form *core.WalletForm) error              { return nil }
func (c *TCore) OpenWallet(assetID uint32, pw string) error            { return nil }
func (c *TCore) Wallets() []*core.WalletStatus                         { return nil }
func (c *TCore) User() *core.User                                      { return nil }

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
		fmt.Println("here")
		return 0, io.EOF
	}
	copy(p, r.msg)
	if len(p) < len(r.msg) {
		r.msg = r.msg[:len(p)]
		return len(p), nil
	} else {
		l := len(r.msg)
		r.msg = nil
		return l, io.EOF
	}
}

func (r *TReader) Close() error { return nil }

type TConn struct {
	msg []byte
}

func (c *TConn) ReadMessage() (int, []byte, error) {
	return 0, nil, nil
}

func (c *TConn) WriteMessage(_ int, msg []byte) error {
	c.msg = msg
	return nil
}

type tLink struct {
	cl   *wsClient
	conn *TConn
}

func newLink() *tLink {
	conn := new(TConn)
	cl := newWSClient("", conn, func(*msgjson.Message) *msgjson.Error { return nil })
	return &tLink{
		cl:   cl,
		conn: conn,
	}
}

func enableLogging() {
	log = slog.NewBackend(os.Stdout).Logger("TEST")
	log.SetLevel(slog.LevelTrace)
}

func (c *TConn) Close() error {
	return nil
}

var tPort int = 5142

func newTServer(t *testing.T, start bool) (*WebServer, *TCore, func()) {
	tPort++
	c := &TCore{}
	var shutdown func()
	ctx, killCtx := context.WithCancel(tCtx)
	s, err := New(c, fmt.Sprintf("localhost:%d", tPort), tLogger, false)
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

func ensureResponse(t *testing.T, s *WebServer, f func(w http.ResponseWriter, r *http.Request), want string, reader *TReader, writer *TWriter, body interface{}) {
	reader.msg, _ = json.Marshal(body)
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
	tLogger = slog.NewBackend(os.Stdout).Logger("TEST")
	tLogger.SetLevel(slog.LevelTrace)
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	doIt := func() int {
		// Not counted as coverage, must test Archiver constructor explicitly.
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestLoadMarket(t *testing.T) {
	enableLogging()
	link := newLink()
	s, tCore, shutdown := newTServer(t, false)
	defer shutdown()
	tBase := uint32(1)
	tQuote := uint32(2)
	tDEX := "abc"
	params := &marketLoad{
		DEX:   tDEX,
		Base:  tBase,
		Quote: tQuote,
	}

	ensureWatching := func(base, quote uint32) {
		// Add a tiny delay here because because it's convenient and this function
		// is called right after a monitoring goroutine is started.
		time.Sleep(time.Millisecond)
		mktID := marketID(base, quote)
		clientCount := 0
		s.mtx.Lock()
		// Make sure there is only one client total
		for _, syncer := range s.syncers {
			clientCount += len(syncer.clients)
		}
		syncer, found := s.syncers[mktID]
		s.mtx.Unlock()

		if clientCount != 1 {
			t.Fatalf("expected 1 client, found %d", clientCount)
		}

		if !found {
			t.Fatalf("no syncer found for market %s", mktID)
		}

		syncer.mtx.Lock()
		_, found = syncer.clients[link.cl.cid]
		syncer.mtx.Unlock()

		if !found {
			t.Fatalf("client not found in syncer list")
		}
	}

	msg, _ := msgjson.NewRequest(1, "a", params)
	ensureErr := func(name string, wantCode int) {
		got := wsLoadMarket(s, link.cl, msg)
		if got == nil {
			t.Fatalf("%s: no error", name)
		}
		if wantCode != got.Code {
			t.Fatalf("%s, wanted %d, got %d", name, wantCode, got.Code)
		}
	}

	// Sync error from core is an error.
	tCore.syncErr = tErr
	ensureErr("sync", msgjson.RPCInternal)
	tCore.syncErr = nil

	rpcErr := wsLoadMarket(s, link.cl, msg)
	if rpcErr != nil {
		t.Fatalf("error loading market: %v", rpcErr)
	}

	// Ensure the client is watching.
	ensureWatching(tBase, tQuote)

	// Load a new market, and make sure the old market was unloaded.
	newBase := uint32(3)
	newQuote := uint32(4)
	params.Base = newBase
	params.Quote = newQuote
	msg, _ = msgjson.NewRequest(2, "a", params)
	rpcErr = wsLoadMarket(s, link.cl, msg)
	if rpcErr != nil {
		t.Fatalf("error loading market: %v", rpcErr)
	}
	ensureWatching(newBase, newQuote)

	// Unsubscribe
	wsUnmarket(nil, link.cl, nil)

	// Ensure there are no clients watching any markets.
	s.mtx.Lock()
	for _, syncer := range s.syncers {
		syncer.mtx.Lock()
		if len(syncer.clients) != 0 {
			t.Fatalf("Syncer for market %d-%d still has %d clients after unMarket", syncer.base, syncer.quote, len(syncer.clients))
		}
		syncer.mtx.Unlock()
	}
	s.mtx.Unlock()

	// Balance error.
	tCore.balanceErr = tErr
	ensureErr("balance", msgjson.RPCInternal)
	tCore.balanceErr = nil
}

func TestAPIRegister(t *testing.T) {
	enableLogging()
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore, shutdown := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s, s.apiRegister, want, reader, writer, body)
	}

	goodBody := &core.Registration{
		DEX:      "test",
		Password: "pass",
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
	ensure(`{"ok":false,"msg":"registration error: test error"}`)
	tCore.regErr = nil
}

func TestAPILogin(t *testing.T) {
	enableLogging()
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore, shutdown := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s, s.apiLogin, want, reader, writer, body)
	}

	goodBody := &loginForm{
		DEX:  "abc",
		Pass: "def",
	}
	body = goodBody
	ensure(`{"ok":true}`)

	// Login error
	tCore.loginErr = tErr
	ensure(`{"ok":false,"msg":"login error: test error"}`)
	tCore.loginErr = nil
}

func TestHandleMessage(t *testing.T) {
	link := newLink()
	s, _, shutdown := newTServer(t, false)
	defer shutdown()
	var msg *msgjson.Message

	ensureErr := func(name string, wantCode int) {
		got := s.handleMessage(link.cl, msg)
		if got == nil {
			t.Fatalf("%s: no error", name)
		}
		if wantCode != got.Code {
			t.Fatalf("%s, wanted %d, got %d", name, wantCode, got.Code)
		}
	}

	// Send a response, which is unsupported on the web server.
	msg, _ = msgjson.NewResponse(1, nil, nil)
	ensureErr("bad route", msgjson.UnknownMessageType)

	// Unknown route.
	msg, _ = msgjson.NewRequest(1, "123", nil)
	ensureErr("bad route", msgjson.UnknownMessageType)

	// Set the route correctly.
	wsHandlers["123"] = func(*WebServer, *wsClient, *msgjson.Message) *msgjson.Error {
		return nil
	}

	rpcErr := s.handleMessage(link.cl, msg)
	if rpcErr != nil {
		t.Fatalf("error for good message: %d: %s", rpcErr.Code, rpcErr.Message)
	}
}

func TestClientMap(t *testing.T) {
	s, _, shutdown := newTServer(t, true)
	conn := new(TConn)

	go s.websocketHandler(conn, "someip")
	time.Sleep(time.Millisecond)

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
