//go:build !live
// +build !live

package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
)

var (
	tCtx  context.Context
	unbip = dex.BipIDSymbol
)

type TCore struct {
	syncFeed   core.BookFeed
	syncErr    error
	notHas     bool
	notRunning bool
	notOpen    bool
}

func (c *TCore) SyncBook(dex string, base, quote uint32) (core.BookFeed, error) {
	return c.syncFeed, c.syncErr
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

func (c *TConn) SetReadLimit(int64) {}

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
	ipk := dex.IPKey{16, 16, 120, 120 /* ipv6 1010:7878:: */}
	cl := newWSClient(ipk.String(), conn, func(*msgjson.Message) *msgjson.Error { return nil }, dex.StdOutLogger("ws_TEST", dex.LevelTrace))
	return &tLink{
		cl:   cl,
		conn: conn,
	}
}

func newTServer() (*Server, *TCore) {
	c := &TCore{}
	return New(c, dex.StdOutLogger("TEST", dex.LevelTrace)), c
}

type tBookFeed struct{}

func (*tBookFeed) Next() <-chan *core.BookUpdate {
	return make(chan *core.BookUpdate, 1)
}
func (*tBookFeed) Close() {}
func (*tBookFeed) Candles(dur string) error {
	return nil
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
	srv, tCore := newTServer()
	// NOTE that Server is not running. We are just using the handleMessage
	// method to route to the proper handler with a configured logger and Core.

	link := newLink()
	linkWg, err := link.cl.Connect(tCtx)
	if err != nil {
		t.Fatalf("WSLink Start: %v", err)
	}

	// This test is not running WebServer or calling handleWS/websocketHandler,
	// so manually stop the marketSyncer started by wsLoadMarket and the WSLink
	// before returning from this test.
	defer func() {
		link.cl.feed.loop.Stop()
		link.cl.feed.loop.WaitForShutdown()
		link.cl.Disconnect()
		linkWg.Wait()
	}()

	params := &marketLoad{
		Host:  "abc",
		Base:  uint32(1),
		Quote: uint32(2),
	}

	subscription, _ := msgjson.NewRequest(1, "loadmarket", params)

	ensureGood := func() {
		t.Helper()
		// Create a new feed for every request because a Close()d feed cannot be
		// reused.
		tCore.syncFeed = &tBookFeed{}
		msgErr := srv.handleMessage(link.cl, subscription)
		if msgErr != nil {
			t.Fatalf("'loadmarket' error: %d: %s", msgErr.Code, msgErr.Message)
		}
		if link.cl.feed.loop == nil {
			t.Fatalf("nil book feed waiter after 'loadmarket'")
		}
	}

	// Initial success.
	ensureGood()

	// Unsubscribe.
	unsub, _ := msgjson.NewRequest(2, "unmarket", nil)
	msgErr := srv.handleMessage(link.cl, unsub)
	if msgErr != nil {
		t.Fatalf("'unmarket' error: %d: %s", msgErr.Code, msgErr.Message)
	}

	if link.cl.feed != nil {
		t.Fatalf("non-nil book feed waiter after 'unmarket'")
	}

	// Make sure a sync error propagates.
	tCore.syncErr = fmt.Errorf("expected dummy error")
	msgErr = srv.handleMessage(link.cl, subscription)
	if msgErr == nil {
		t.Fatalf("no handleMessage error from Sync error")
	}
	tCore.syncErr = nil

	// Success again.
	ensureGood()
}

func TestHandleMessage(t *testing.T) {
	link := newLink()
	srv, _ := newTServer()

	// NOTE: link is not started because the handlers in this test do not
	// actually use it.

	var msg *msgjson.Message

	ensureErr := func(name string, wantCode int) {
		got := srv.handleMessage(link.cl, msg)
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

	rpcErr := srv.handleMessage(link.cl, msg)
	if rpcErr != nil {
		t.Fatalf("error for good message: %d: %s", rpcErr.Code, rpcErr.Message)
	}
}

func TestClientMap(t *testing.T) {
	srv, _ := newTServer()
	resp := make(chan []byte, 1)
	conn := &TConn{
		respReady: resp,
		close:     make(chan struct{}, 1),
	}
	// msg.ID == 0 gets an error response, which can be discarded.
	read, _ := json.Marshal(msgjson.Message{ID: 0})
	conn.addRead(read)

	// Create the context that the http request handler would receive.
	ctx, shutdown := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		ipk := dex.IPKey{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255, 255 /* ipv4 */, 127, 0, 0, 1}
		srv.connect(ctx, conn, ipk.String())
		wg.Done()
	}()

	// When a response to our dummy message is received, the client should be in
	// RPCServer's client map.
	<-resp

	var cl *wsClient
	srv.clientsMtx.Lock()
	i := len(srv.clients)
	if i != 1 {
		t.Fatalf("expected 1 client in server map, found %d", i)
	}
	for _, c := range srv.clients {
		cl = c
		break
	}
	srv.clientsMtx.Unlock()

	// Close the server and make sure the connection is closed.
	shutdown()
	wg.Wait() // websocketHandler since it's using log
	if !cl.Off() {
		t.Fatal("connection not closed on server shutdown")
	}
}
