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
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/ws"
	"github.com/decred/slog"
)

var tErr = fmt.Errorf("test error")

type TCore struct {
	balanceErr error
	syncErr    error
	regErr     error
	loginErr   error
}

func (c *TCore) ListMarkets() []*core.MarketInfo     { return nil }
func (c *TCore) Register(r *core.Registration) error { return c.regErr }
func (c *TCore) Login(dex, pw string) error          { return c.loginErr }
func (c *TCore) Sync(dex, mkt string) (*core.OrderBook, chan *core.BookUpdate, error) {
	return nil, nil, c.syncErr
}
func (c *TCore) Unsync(dex, mkt string)          {}
func (c *TCore) Balance(string) (float64, error) { return 0, c.balanceErr }

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
	link *ws.WSLink
	conn *TConn
}

func newLink() *tLink {
	conn := new(TConn)
	link := ws.NewWSLink("", conn, time.Hour, func(*msgjson.Message) *msgjson.Error { return nil })
	return &tLink{
		link: link,
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

func newTServer() (*webServer, *TCore) {
	c := &TCore{}
	return &webServer{
		ctx:  context.Background(),
		core: c,
	}, c
}

func ensureResponse(t *testing.T, s *webServer, f func(w http.ResponseWriter, r *http.Request), want string, reader *TReader, writer *TWriter, body interface{}) {
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

func TestLoadMarket(t *testing.T) {
	enableLogging()
	link := newLink()
	s, tCore := newTServer()
	tMkt := "DEF-GHI"
	params := &marketLoad{
		DEX:    "abc",
		Market: tMkt,
	}
	msg, _ := msgjson.NewRequest(1, "a", params)
	rpcErr := wsLoadMarket(s, link.link, msg)
	if rpcErr != nil {
		t.Fatalf("error loading market: %v", rpcErr)
	}

	// Get the current marketSyncer
	currentMkt := s.openMarket
	if currentMkt.market != tMkt {
		t.Fatalf("wrong market stored. expected %s, got %s", tMkt, currentMkt.market)
	}

	// Load a new market, and make sure the old market was unloaded.
	newMkt := "JKL-MNO"
	params.Market = newMkt
	msg, _ = msgjson.NewRequest(2, "a", params)
	rpcErr = wsLoadMarket(s, link.link, msg)
	if rpcErr != nil {
		t.Fatalf("error loading market: %v", rpcErr)
	}
	currentMkt = s.openMarket
	if currentMkt.market != newMkt {
		t.Fatalf("wrong market stored when replacing. expected %s, got %s", newMkt, currentMkt.market)
	}

	ensureErr := func(name string, wantCode int) {
		got := wsLoadMarket(s, link.link, msg)
		if got == nil {
			t.Fatalf("%s: no error", name)
		}
		if wantCode != got.Code {
			t.Fatalf("%s, wanted %d, got %d", name, wantCode, got.Code)
		}
	}

	// Pass a bad market name.
	params.Market = "ABC"
	msg, _ = msgjson.NewRequest(3, "a", params)
	ensureErr("market name", msgjson.UnknownMarketError)
	params.Market = newMkt
	msg, _ = msgjson.NewRequest(3, "a", params)

	// Balance error.
	tCore.balanceErr = tErr
	ensureErr("balance", msgjson.RPCInternal)
	tCore.balanceErr = nil

	// Sync error from core is an error.
	tCore.syncErr = tErr
	ensureErr("balance", msgjson.RPCInternal)
	tCore.syncErr = nil

}

func TestAPIRegister(t *testing.T) {
	enableLogging()
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore := newTServer()

	ensure := func(want string) {
		ensureResponse(t, s, s.apiRegister, want, reader, writer, body)
	}

	goodBody := &core.Registration{
		DEX:        "test",
		Wallet:     "test",
		WalletPass: "test",
		RPCAddr:    "test",
		RPCUser:    "test",
		RPCPass:    "test",
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
	s, tCore := newTServer()

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
	s, _ := newTServer()
	var msg *msgjson.Message

	ensureErr := func(name string, wantCode int) {
		got := s.handleMessage(link.link, msg)
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
	wsHandlers["123"] = func(*webServer, *ws.WSLink, *msgjson.Message) *msgjson.Error {
		return nil
	}

	rpcErr := s.handleMessage(link.link, msg)
	if rpcErr != nil {
		t.Fatalf("error for good message: %d: %s", rpcErr.Code, rpcErr.Message)
	}
}
