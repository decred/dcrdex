// +build !live

package websocket

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

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
)

var (
	tErr  = fmt.Errorf("test error")
	tCtx  context.Context
	unbip = dex.BipIDSymbol
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
	syncBook   *core.OrderBook
	syncFeed   *core.BookFeed
	syncErr    error
	notHas     bool
	notRunning bool
	notOpen    bool
}

func (c *TCore) SyncBook(dex string, base, quote uint32) (*core.OrderBook, *core.BookFeed, error) {
	return c.syncBook, c.syncFeed, c.syncErr
}
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
func (c *TCore) AckNotes(ids []dex.Bytes) {}

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
		close:     make(chan struct{}, 1),
	}
	cl := newWSClient("", conn, func(*msgjson.Message) *msgjson.Error { return nil })
	return &tLink{
		cl:   cl,
		conn: conn,
	}
}

func newTServer(t *testing.T) (*Server, *TCore, func()) {
	c := &TCore{}
	ctx, killCtx := context.WithCancel(tCtx)
	s := New(ctx, c, dex.StdOutLogger("TEST", dex.LevelTrace))
	return s, c, killCtx
}

func ensureResponse(t *testing.T, s *Server, f func(w http.ResponseWriter, r *http.Request), want string, reader *TReader, writer *TWriter, body interface{}) {
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
	s, tCore, shutdown := newTServer(t)
	defer shutdown()
	linkWg, err := link.cl.Connect(tCtx)
	if err != nil {
		t.Fatalf("WSLink Start: %v", err)
	}

	// This test is not running WebServer or calling handleWS/websocketHandler,
	// so manually stop the marketSyncer started by wsLoadMarket and the WSLink
	// before returning from this test.
	defer func() {
		link.cl.feedLoop.Stop()
		link.cl.feedLoop.WaitForShutdown()
		link.cl.Disconnect()
		linkWg.Wait()
	}()

	params := &marketLoad{
		Host:  "abc",
		Base:  uint32(1),
		Quote: uint32(2),
	}

	subscription, _ := msgjson.NewRequest(1, "loadmarket", params)
	tCore.syncBook = &core.OrderBook{}

	extractMessage := func() *msgjson.Message {
		t.Helper()
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
		t.Helper()
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

func TestHandleMessage(t *testing.T) {
	link := newLink()
	s, _, shutdown := newTServer(t)
	defer shutdown()

	// NOTE: link is not started because the handlers in this test do not
	// actually use it.

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
	wsHandlers["123"] = func(*Server, *wsClient, *msgjson.Message) *msgjson.Error {
		return nil
	}

	rpcErr := s.handleMessage(link.cl, msg)
	if rpcErr != nil {
		t.Fatalf("error for good message: %d: %s", rpcErr.Code, rpcErr.Message)
	}
}

func TestClientMap(t *testing.T) {
	s, _, shutdown := newTServer(t)
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
		s.connect(conn, "someip")
		wg.Done()
	}()

	// When a response to our dummy message is received, the client should be in
	// RPCServer's client map.
	<-resp

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
		t.Fatal("connection not closed on server shutdown")
	}
}
