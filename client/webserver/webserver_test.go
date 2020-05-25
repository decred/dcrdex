// +build !live

package webserver

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/order"
	"github.com/decred/slog"
)

var (
	tErr    = fmt.Errorf("test error")
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

func (c *tCoin) Confirmations() (uint32, error) {
	return c.confs, c.confsErr
}

func (c *tCoin) Redeem() dex.Bytes {
	return nil
}

type TCore struct {
	balanceErr      error
	syncBook        *core.OrderBook
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

func (c *TCore) Exchanges() map[string]*core.Exchange       { return nil }
func (c *TCore) GetFee(string, string) (uint64, error)      { return 1e8, c.getFeeErr }
func (c *TCore) Register(r *core.RegisterForm) error        { return c.regErr }
func (c *TCore) InitializeClient(pw []byte) error           { return c.initErr }
func (c *TCore) Login(pw []byte) (*core.LoginResult, error) { return &core.LoginResult{}, c.loginErr }
func (c *TCore) Sync(dex string, base, quote uint32) (*core.OrderBook, *core.BookFeed, error) {
	return c.syncBook, c.syncFeed, c.syncErr
}
func (c *TCore) Book(dex string, base, quote uint32) (*core.OrderBook, error) {
	return &core.OrderBook{}, nil
}
func (c *TCore) Balance(uint32) (uint64, error) { return 0, c.balanceErr }
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
func (c *TCore) OpenWallet(assetID uint32, pw []byte) error { return c.openWalletErr }
func (c *TCore) CloseWallet(assetID uint32) error           { return c.closeWalletErr }
func (c *TCore) ConnectWallet(assetID uint32) error         { return nil }
func (c *TCore) Wallets() []*core.WalletState               { return nil }
func (c *TCore) User() *core.User                           { return nil }
func (c *TCore) SupportedAssets() map[uint32]*core.SupportedAsset {
	return make(map[uint32]*core.SupportedAsset)
}
func (c *TCore) Withdraw(pw []byte, assetID uint32, value uint64) (asset.Coin, error) {
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

func (c *TCore) Cancel(pw []byte, sid string) error { return nil }

func (c *TCore) NotificationFeed() <-chan core.Notification { return make(chan core.Notification, 1) }

func (c *TCore) AckNotes(ids []dex.Bytes) {}

func (c *TCore) Logout() error { return c.logoutErr }

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
	} else {
		l := len(r.msg)
		r.msg = nil
		return l, io.EOF
	}
}

func (r *TReader) Close() error { return nil }

type TConn struct {
	msg       []byte
	reads     [][]byte      // data for ReadMessage
	respReady chan []byte   // signal from WriteMessage
	close     chan struct{} // Close tells ReadMessage to return with error
}

var readTimeout = 10 * time.Second // ReadMessage must not return constantly with nothing

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

func (c *TConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func (c *TConn) WriteControl(messageType int, data []byte, deadline time.Time) error {
	return nil
}

func (c *TConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *TConn) WriteMessage(_ int, msg []byte) error {
	c.msg = msg
	select {
	case c.respReady <- msg:
	default:
	}
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
	cl := newWSClient("", conn, func(*msgjson.Message) *msgjson.Error { return nil })
	return &tLink{
		cl:   cl,
		conn: conn,
	}
}

var tPort int = 5142

func newTServer(t *testing.T, start bool) (*WebServer, *TCore, func()) {
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
	link := newLink()
	s, tCore, shutdown := newTServer(t, false)
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
	tCore.syncErr = tErr
	msgErr = s.handleMessage(link.cl, subscription)
	if msgErr == nil {
		t.Fatalf("no handleMessage error from Sync error")
	}
	tCore.syncErr = nil

	// Success again.
	ensureGood()
}

func TestAPIRegister(t *testing.T) {
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore, shutdown := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s, s.apiRegister, want, reader, writer, body)
	}

	goodBody := &core.RegisterForm{
		URL:     "test",
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
	ensure(`{"ok":false,"msg":"registration error: test error"}`)
	tCore.regErr = nil
}

func TestAPILogin(t *testing.T) {
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore, shutdown := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s, s.apiLogin, want, reader, writer, body)
	}

	goodBody := &loginForm{
		Pass: encode.PassBytes("def"),
	}
	body = goodBody
	ensure(`{"ok":true,"notes":null}`)

	// Login error
	tCore.loginErr = tErr
	ensure(`{"ok":false,"msg":"login error: test error"}`)
	tCore.loginErr = nil
}

func TestWithdraw(t *testing.T) {
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore, shutdown := newTServer(t, false)
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
	s, tCore, shutdown := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s, s.apiInit, want, reader, writer, body)
	}

	goodBody := &loginForm{
		Pass: encode.PassBytes("def"),
	}
	body = goodBody
	ensure(`{"ok":true,"notes":null}`)

	// Initialization error
	tCore.initErr = tErr
	ensure(`{"ok":false,"msg":"initialization error: test error"}`)
	tCore.initErr = nil
}

func TestAPIGetFee(t *testing.T) {
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore, shutdown := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s, s.apiGetFee, want, reader, writer, body)
	}

	body = &registration{URL: "somedexaddress.org"}
	ensure(`{"ok":true,"fee":100000000}`)

	// getFee error
	tCore.getFeeErr = tErr
	ensure(`{"ok":false,"msg":"test error"}`)
	tCore.getFeeErr = nil
}

func TestAPINewWallet(t *testing.T) {
	writer := new(TWriter)
	var body interface{}
	reader := new(TReader)
	s, tCore, shutdown := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s, s.apiNewWallet, want, reader, writer, body)
	}

	body = &newWalletForm{
		Account: "account",
		Pass:    encode.PassBytes("123"),
	}
	tCore.notHas = true
	ensure(`{"ok":true}`)

	tCore.notHas = false
	ensure(`{"ok":false,"msg":"already have a wallet for btc"}`)
	tCore.notHas = true

	tCore.createWalletErr = tErr
	ensure(`{"ok":false,"msg":"error creating btc wallet: test error"}`)
	tCore.createWalletErr = nil

	tCore.notHas = false
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
	resp := make(chan []byte, 1)
	conn := &TConn{
		respReady: resp,
		close:     make(chan struct{}, 1),
	}
	// msg.ID == 0 gets an error response, which can be discarded.
	read, _ := json.Marshal(msgjson.Message{ID: 0})
	conn.addRead(read)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		s.websocketHandler(conn, "someip")
		wg.Done()
	}()

	// When a response to our dummy message is received, the client should be in
	// RPCServer's client map.
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
	wg.Wait() // websocketHandler since it's using log
	if !cl.Off() {
		t.Fatalf("connection not closed on server shutdown")
	}
}

func TestAPILogout(t *testing.T) {
	writer := new(TWriter)
	reader := new(TReader)
	s, tCore, shutdown := newTServer(t, false)
	defer shutdown()

	ensure := func(want string) {
		ensureResponse(t, s, s.apiLogout, want, reader, writer, nil)
	}
	ensure(`{"ok":true}`)

	// Logout error
	tCore.logoutErr = tErr
	ensure(`{"ok":false,"msg":"logout error: test error"}`)
	tCore.logoutErr = nil
}
