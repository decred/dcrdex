package comms

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/ws"
	"github.com/gorilla/websocket"
)

var (
	tErr    = fmt.Errorf("test error")
	testCtx context.Context
	tLogger = dex.StdOutLogger("TCOMMS", dex.LevelTrace)
)

func newServer() *Server {
	return &Server{
		clients:     make(map[uint64]*wsLink),
		quarantine:  make(map[dex.IPKey]time.Time),
		dataEnabled: 1,
	}
}

func giveItASecond(f func() bool) bool {
	ticker := time.NewTicker(time.Millisecond)
	timeout := time.NewTimer(time.Second)
	for {
		if f() {
			return true
		}
		select {
		case <-timeout.C:
			return false
		default:
		}
		<-ticker.C
	}
}

func readChannel(t *testing.T, tag string, c chan interface{}) interface{} {
	t.Helper()
	select {
	case i := <-c:
		return i
	case <-time.NewTimer(time.Second).C:
		t.Fatalf("%s: didn't read channel", tag)
	}
	return nil
}

func decodeResponse(t *testing.T, b []byte) *msgjson.ResponsePayload {
	t.Helper()
	msg, err := msgjson.DecodeMessage(b)
	if err != nil {
		t.Fatalf("error decoding last message (%s): %v", string(b), err)
	}
	resp, err := msg.Response()
	if err != nil {
		t.Fatalf("error decoding response payload: %v", err)
	}
	return resp
}

type wsConnStub struct {
	msg      chan []byte
	quit     chan struct{}
	close    int
	recv     chan []byte
	writeMtx sync.Mutex
	writeErr error
}

func (conn *wsConnStub) addChan() {
	conn.recv = make(chan []byte)
}

func (conn *wsConnStub) wait(t *testing.T, tag string) {
	t.Helper()
	select {
	case <-conn.recv:
	case <-time.NewTimer(time.Second).C:
		t.Fatalf("%s - wait timeout", tag)
	}
}

func newWsStub() *wsConnStub {
	return &wsConnStub{
		msg: make(chan []byte),
		// recv is nil unless a test wants to receive
		quit: make(chan struct{}),
	}
}

func (conn *wsConnStub) setWriteErr(err error) {
	conn.writeMtx.Lock()
	conn.writeErr = err
	conn.writeMtx.Unlock()
}

// nonEOF can specify a particular error should be returned through ReadMessage.
var nonEOF = make(chan struct{})
var pongTrigger = []byte("pong")

func (conn *wsConnStub) ReadMessage() (int, []byte, error) {
	var b []byte
	select {
	case b = <-conn.msg:
		if bytes.Equal(b, pongTrigger) {
			return websocket.PongMessage, []byte{}, nil
		}
	case <-conn.quit:
		return 0, nil, io.EOF
	case <-testCtx.Done():
		return 0, nil, io.EOF
	case <-nonEOF:
		close(conn.quit)
		return 0, nil, fmt.Errorf("test nonEOF error")
	}
	return 0, b, nil
}

func (conn *wsConnStub) WriteMessage(msgType int, msg []byte) error {
	conn.writeMtx.Lock()
	defer conn.writeMtx.Unlock()
	if msgType == websocket.PingMessage {
		select {
		case conn.msg <- pongTrigger:
		default:
		}
		return nil
	}
	// Send the message if their is a receiver for the current test.
	if conn.recv != nil {
		conn.recv <- msg
	}
	if conn.writeErr == nil {
		return nil
	}
	err := conn.writeErr
	conn.writeErr = nil
	return err
}

func (conn *wsConnStub) SetReadLimit(int64) {}

func (conn *wsConnStub) SetWriteDeadline(t time.Time) error {
	return nil // TODO implement and test write timeouts
}

func (conn *wsConnStub) SetReadDeadline(t time.Time) error {
	return nil
}

func (conn *wsConnStub) WriteControl(messageType int, data []byte, deadline time.Time) error {
	return nil
}

func (conn *wsConnStub) Close() error {
	select {
	case <-conn.quit:
	default:
		close(conn.quit)
	}
	conn.close++
	return nil
}

func dummyRPCHandler(_ Link, _ *msgjson.Message) *msgjson.Error {
	return nil
}

var reqID uint64

func makeReq(route, msg string) *msgjson.Message {
	reqID++
	req, _ := msgjson.NewRequest(reqID, route, json.RawMessage(msg))
	return req
}

func makeResp(id uint64, msg string) *msgjson.Message {
	resp, _ := msgjson.NewResponse(id, json.RawMessage(msg), nil)
	return resp
}

func makeNtfn(route, msg string) *msgjson.Message {
	ntfn, _ := msgjson.NewNotification(route, json.RawMessage(msg))
	return ntfn
}

func sendToConn(t *testing.T, conn *wsConnStub, method, msg string) {
	encMsg, err := json.Marshal(makeReq(method, msg))
	if err != nil {
		t.Fatalf("error encoding %s request: %v", method, err)
	}
	conn.msg <- encMsg
}

func sendReplace(t *testing.T, conn *wsConnStub, thing interface{}, old, new string) {
	enc, err := json.Marshal(thing)
	if err != nil {
		t.Fatalf("error encoding thing for sendReplace: %v", err)
	}
	s := string(enc)
	s = strings.ReplaceAll(s, old, new)
	conn.msg <- []byte(s)
}

func newTestDEXClient(addr string, rootCAs *x509.CertPool) (*websocket.Conn, error) {
	uri, err := url.Parse(addr)
	if err != nil {
		return nil, fmt.Errorf("error parsing url: %w", err)
	}

	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment, // Same as DefaultDialer.
		HandshakeTimeout: 10 * time.Second,          // DefaultDialer is 45 seconds.
		TLSClientConfig: &tls.Config{
			RootCAs:            rootCAs,
			InsecureSkipVerify: true,
			ServerName:         uri.Hostname(),
		},
	}

	conn, _, err := dialer.Dial(addr, nil)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func TestMain(m *testing.M) {
	var shutdown func()
	testCtx, shutdown = context.WithCancel(context.Background())
	defer shutdown()
	// Register dummy handlers for the HTTP routes.
	for _, route := range []string{msgjson.ConfigRoute, msgjson.SpotsRoute, msgjson.CandlesRoute, msgjson.OrderBookRoute} {
		RegisterHTTP(route, func(interface{}) (interface{}, error) { return nil, nil })
	}
	UseLogger(tLogger)
	os.Exit(m.Run())
}

// method strings cannot be empty.
func TestRoute_PanicsEmptyString(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("no panic on registering empty string method")
		}
	}()
	Route("", dummyRPCHandler)
}

// methods cannot be registered more than once.
func TestRoute_PanicsDoubleRegistry(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("no panic on registering empty string method")
		}
	}()
	Route("somemethod", dummyRPCHandler)
	Route("somemethod", dummyRPCHandler)
}

// Test the server with a stub for the client connections.
func TestClientRequests(t *testing.T) {
	server := newServer()
	var wg sync.WaitGroup
	defer func() {
		server.disconnectClients()
		wg.Wait()
	}()
	var client *wsLink
	var conn *wsConnStub
	stubAddr := dex.IPKey{}
	copy(stubAddr[:], []byte("testaddr"))
	sendToServer := func(method, msg string) { sendToConn(t, conn, method, msg) }

	waitForShutdown := func(tag string, f func()) {
		needCount := server.clientCount() - 1
		f()
		if !giveItASecond(func() bool {
			return server.clientCount() == needCount
		}) {
			t.Fatalf("%s: waitForShutdown failed", tag)
		}
	}

	// Register all methods before sending any requests.
	// 'getclient' grabs the server's link.
	srvChan := make(chan interface{})
	Route("getclient", func(c Link, _ *msgjson.Message) *msgjson.Error {
		client, ok := c.(*wsLink)
		if !ok {
			t.Fatalf("failed to assert client type")
		}
		srvChan <- client
		return nil
	})
	getClient := func() {
		encReq, _ := json.Marshal(makeReq("getclient", `{}`))
		conn.msg <- encReq
		client = readChannel(t, "getClient", srvChan).(*wsLink)
	}

	// Check request parses the request to a map of strings.
	Route("checkrequest", func(c Link, msg *msgjson.Message) *msgjson.Error {
		if string(msg.Payload) != `{"key":"value"}` {
			t.Fatalf("wrong request: %s", string(msg.Payload))
		}
		if client.id != c.ID() {
			t.Fatalf("client ID mismatch. %d != %d", client.id, c.ID())
		}
		srvChan <- nil
		return nil
	})
	// 'checkinvalid' should never be run, since the request has invalid
	// formatting.
	var passed bool
	Route("checkinvalid", func(_ Link, _ *msgjson.Message) *msgjson.Error {
		passed = true
		return nil
	})
	// 'error' returns an Error.
	Route("error", func(_ Link, _ *msgjson.Message) *msgjson.Error {
		return msgjson.NewError(550, "somemessage")
	})
	// 'ban' quarantines the user using the RPCQuarantineClient error code.
	Route("ban", func(c Link, req *msgjson.Message) *msgjson.Error {
		rpcErr := msgjson.NewError(msgjson.RPCQuarantineClient, "user quarantined")
		errMsg, _ := msgjson.NewResponse(req.ID, nil, rpcErr)
		err := c.Send(errMsg)
		if err != nil {
			t.Fatalf("ban route send error: %v", err)
		}
		c.Banish()
		return nil
	})
	var httpSeen uint32
	RegisterHTTP("httproute", func(thing interface{}) (interface{}, error) {
		atomic.StoreUint32(&httpSeen, 1)
		srvChan <- nil
		return struct{}{}, nil
	})

	// A helper function to reconnect to the server (new comm) and grab the
	// server's link (new client).
	reconnect := func() {
		conn = newWsStub()

		needCount := server.clientCount() + 1
		wg.Add(1)
		go func() {
			defer wg.Done()
			server.websocketHandler(testCtx, conn, stubAddr)
		}()

		if !giveItASecond(func() bool {
			return server.clientCount() == needCount
		}) {
			t.Fatalf("failed to add client")
		}

		getClient()
	}

	reconnect()

	// Check that the request is parsed as expected.
	sendToServer("checkrequest", `{"key":"value"}`)
	readChannel(t, "checkrequest", srvChan)
	// Send invalid params, and make sure the server doesn't pass the message. The
	// server will not disconnect the client.
	conn.addChan()

	ensureReplaceFails := func(old, new string) {
		sendReplace(t, conn, makeReq("checkinvalid", old), old, new)
		<-conn.recv
		if passed {
			t.Fatalf("invalid request passed to handler")
		}
	}

	ensureReplaceFails(`{"a":"b"}`, "?")
	if client.Off() {
		t.Fatalf("client unexpectedly disconnected after invalid message")
	}

	// Send the invalid message again, but error out on the server's WriteMessage
	// attempt. The server should disconnect the client in this case.
	conn.setWriteErr(tErr)
	waitForShutdown("rpc error", func() {
		ensureReplaceFails(`{"a":"b"}`, "?")
	})

	// Shut the client down. Check the on flag.
	reconnect()
	waitForShutdown("flag set", func() {
		client.Disconnect()
	})

	// Reconnect and try shutting down with non-EOF error.
	reconnect()
	waitForShutdown("non-EOF", func() {
		nonEOF <- struct{}{}
	})

	// Try a non-existent handler. This should not result in a disconnect.
	reconnect()
	conn.addChan()
	sendToServer("nonexistent", "{}")
	conn.wait(t, "bad path without error")
	if client.Off() {
		t.Fatalf("client unexpectedly disconnected after invalid method")
	}

	// Again, but with an WriteMessage error when sending error to client. This
	// should result in a disconnection.
	conn.setWriteErr(tErr)
	waitForShutdown("rpc error", func() {
		sendToServer("nonexistent", "{}")
		conn.wait(t, "bad path with error")
	})

	// An RPC error. No disconnect.
	reconnect()
	conn.addChan()
	sendToServer("error", "{}")
	conn.wait(t, "rpc error")
	if client.Off() {
		t.Fatalf("client unexpectedly disconnected after rpc error")
	}

	// Return a user quarantine error.
	waitForShutdown("ban", func() {
		sendToServer("ban", "{}")
		conn.wait(t, "ban")
	})
	if !server.isQuarantined(stubAddr) {
		t.Fatalf("server has not marked client as quarantined")
	}
	// A call to Send should return ErrPeerDisconnected
	if !errors.Is(client.Send(nil), ws.ErrPeerDisconnected) {
		t.Fatalf("incorrect error for disconnected client")
	}

	// Test that an http request passes.
	reconnect()
	conn.addChan()
	sendToServer("httproute", "{}")
	readChannel(t, "httproute", srvChan)
	if !atomic.CompareAndSwapUint32(&httpSeen, 1, 0) {
		t.Fatalf("HTTP route not hit")
	}
	conn.wait(t, "http route success")

	// Disable HTTP non-critical HTTP routes and try again.
	server.EnableDataAPI(false)
	sendToServer("httproute", "{}")
	resp := decodeResponse(t, <-conn.recv)
	if resp.Error == nil || resp.Error.Code != msgjson.RouteUnavailableError {
		t.Fatalf("no error for disabled HTTP route")
	}
	if atomic.CompareAndSwapUint32(&httpSeen, 1, 0) {
		t.Fatalf("disabled HTTP route hit")
	}

	// Make the route a critical route
	criticalRoutes["httproute"] = true
	sendToServer("httproute", "{}")
	readChannel(t, "httproute", srvChan)
	if !atomic.CompareAndSwapUint32(&httpSeen, 1, 0) {
		t.Fatalf("critical HTTP route not hit")
	}
	conn.wait(t, "critical http route success")

	checkParseError := func() {
		resp := decodeResponse(t, <-conn.recv)
		if resp.Error == nil || resp.Error.Code != msgjson.RPCParseError {
			t.Fatalf("no error after invalid id")
		}
	}

	// Test an invalid ID.
	reconnect()
	conn.addChan()
	msg := makeReq("getclient", `{}`)
	msg.ID = 555
	sendReplace(t, conn, msg, "555", "{}")
	checkParseError()

	// Test null ID
	sendReplace(t, conn, msg, "555", "null")
	checkParseError()

}

func TestClientResponses(t *testing.T) {
	server := newServer()
	var client *wsLink
	var conn *wsConnStub
	stubAddr := dex.IPKey{}
	copy(stubAddr[:], []byte("testaddr"))

	// Register all methods before sending any requests.
	// 'getclient' grabs the server's link.
	srvChan := make(chan interface{})
	Route("grabclient", func(c Link, _ *msgjson.Message) *msgjson.Error {
		client, ok := c.(*wsLink)
		if !ok {
			t.Fatalf("failed to assert client type")
		}
		srvChan <- client
		return nil
	})

	getClient := func() {
		encReq, _ := json.Marshal(makeReq("grabclient", `{}`))
		conn.msg <- encReq
		client = readChannel(t, "grabclient", srvChan).(*wsLink)
	}

	sendToClient := func(route, payload string, f func(Link, *msgjson.Message), expiration time.Duration, expire func()) uint64 {
		req := makeReq(route, payload)
		err := client.Request(req, f, expiration, expire)
		if err != nil {
			t.Logf("sendToClient error: %v", err)
		}
		return req.ID
	}

	respondToServer := func(id uint64, msg string) {
		encResp, err := json.Marshal(makeResp(id, msg))
		if err != nil {
			t.Fatalf("error encoding %v (%T) request: %v", id, id, err)
		}
		conn.msg <- encResp
	}

	var wg sync.WaitGroup
	reconnect := func() {
		conn = newWsStub()
		wg.Add(1)
		go func() {
			defer wg.Done()
			server.websocketHandler(testCtx, conn, stubAddr)
		}()
		getClient()
	}
	reconnect()

	defer func() {
		server.disconnectClients()
		wg.Wait()
	}()

	// Test Broadcast
	conn.addChan()                                   // for WriteMessage in this test
	server.Broadcast(makeNtfn("someNote", `"blah"`)) // async conn.recv <- msg send
	msgBytes := <-conn.recv
	msg, err := msgjson.DecodeMessage(msgBytes)
	if err != nil {
		t.Fatalf("error decoding last message: %v", err)
	}
	var note string
	err = json.Unmarshal(msg.Payload, &note)
	if err != nil {
		return
	}
	if note != "blah" {
		t.Errorf("wrong note: %s", note)
	}

	// Send a request from the server to the client, setting a flag when the
	// client responds.
	id := sendToClient("looptest", `{}`, func(_ Link, _ *msgjson.Message) {
		srvChan <- nil
	}, time.Hour, func() {})

	// Respond to the server
	respondToServer(id, `{}`)
	readChannel(t, "looptest", srvChan)
	<-conn.recv

	checkParseError := func(tag string) {
		msg, err := msgjson.DecodeMessage(<-conn.recv)
		if err != nil {
			t.Fatalf("error decoding last message (%s): %v", tag, err)
		}
		resp, err := msg.Response()
		if err != nil {
			t.Fatalf("error decoding response (%s): %v", tag, err)
		}
		if resp.Error == nil || resp.Error.Code != msgjson.RPCParseError {
			t.Fatalf("no error after %s", tag)
		}
	}

	// Test an invalid id type.
	sendReplace(t, conn, makeResp(1, `{}`), `:1`, `:0`)
	checkParseError("invalid id")

	// Send an invalid payload.
	old := `{"a":"b"}`
	sendReplace(t, conn, makeResp(id, old), old, `?`)
	checkParseError("invalid payload")

	// check the response handler expiration
	client.respHandlers = make(map[uint64]*responseHandler)
	expiredID := sendToClient("expiration", `{}`, func(_ Link, _ *msgjson.Message) {},
		200*time.Millisecond, func() { t.Log("Expired (good).") })
	<-conn.recv
	// The responseHandler map should contain the ntfn ID since expiry has not
	// yet arrived.
	client.reqMtx.Lock()
	_, found := client.respHandlers[expiredID]
	if !found {
		t.Fatalf("response handler not found")
	}
	if len(client.respHandlers) != 1 {
		t.Fatalf("expected 1 response handler, found %d", len(client.respHandlers))
	}
	client.reqMtx.Unlock()

	time.Sleep(250 * time.Millisecond) // >> 200ms - 10ms
	client.reqMtx.Lock()
	if len(client.respHandlers) != 0 {
		t.Fatalf("expired response handler not pruned")
	}
	_, found = client.respHandlers[expiredID]
	if found {
		t.Fatalf("expired response handler still in map")
	}
	client.reqMtx.Unlock()
}

func TestOnline(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "example")
	if err != nil {
		t.Fatalf("TempDir error: %v", err)
	}
	defer os.RemoveAll(tempDir) // clean up

	keyPath := filepath.Join(tempDir, "rpc.key")
	certPath := filepath.Join(tempDir, "rpc.cert")
	pongWait = time.Millisecond * 500
	pingPeriod = (pongWait * 9) / 10
	server, err := NewServer(&RPCConfig{
		ListenAddrs: []string{":0"},
		RPCKey:      keyPath,
		RPCCert:     certPath,
	})
	if err != nil {
		t.Fatalf("server constructor error: %v", err)
	}
	address := "wss://" + server.listeners[0].Addr().String() + "/ws"

	// Register routes before starting server.
	// The 'ok' route returns an affirmative response.
	type okresult struct {
		OK bool `json:"ok"`
	}
	Route("ok", func(c Link, msg *msgjson.Message) *msgjson.Error {
		resp, err := msgjson.NewResponse(msg.ID, &okresult{OK: true}, nil)
		if err != nil {
			return msgjson.NewError(500, err.Error())
		}
		err = c.Send(resp)
		if err != nil {
			return msgjson.NewError(500, err.Error())
		}
		return nil
	})
	// The 'banuser' route quarantines the user.
	banChan := make(chan interface{})
	Route("banuser", func(c Link, req *msgjson.Message) *msgjson.Error {
		rpcErr := msgjson.NewError(msgjson.RPCQuarantineClient, "test quarantine")
		msg, _ := msgjson.NewResponse(req.ID, nil, rpcErr)
		err := c.Send(msg)
		if err != nil {
			t.Fatalf("banuser route send error: %v", err)
		}
		c.Banish()
		banChan <- nil
		return nil
	})

	ssw := dex.NewStartStopWaiter(server)
	ssw.Start(testCtx)
	defer func() {
		ssw.Stop()
		ssw.WaitForShutdown()
	}()

	// Get the SystemCertPool, continue with an empty pool on error
	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	// Read in the cert file
	certs, err := ioutil.ReadFile(certPath)
	if err != nil {
		t.Fatalf("Failed to append %q to RootCAs: %v", certPath, err)
	}

	// Append our cert to the system pool
	if ok := rootCAs.AppendCertsFromPEM(certs); !ok {
		t.Fatalf("No certs appended, using system certs only")
	}

	remoteClient, err := newTestDEXClient(address, rootCAs)
	if err != nil {
		t.Fatalf("remoteClient constructor error: %v", err)
	}

	// A loop to grab responses from the server.
	recv := make(chan interface{})
	go func() {
		for {
			_, r, err := remoteClient.ReadMessage()
			if err == nil {
				recv <- r
			} else {
				recv <- err
				break
			}
		}
	}()

	sendToDEX := func(route, msg string) error {
		b, err := json.Marshal(makeReq(route, msg))
		if err != nil {
			t.Fatalf("error encoding %s request: %v", route, err)
		}
		err = remoteClient.WriteMessage(websocket.TextMessage, b)
		return err
	}

	// Sleep for a couple of pongs to make sure the client doesn't disconnect.
	time.Sleep(pongWait * 2)

	// Positive path.
	err = sendToDEX("ok", "{}")
	if err != nil {
		t.Fatalf("noresponse send error: %v", err)
	}
	b := readChannel(t, "ok", recv).([]byte)

	msg, _ := msgjson.DecodeMessage(b)

	ok := new(okresult)
	err = msg.UnmarshalResult(ok)
	if err != nil {
		t.Fatalf("'ok' response unmarshal error: %v", err)
	}
	if !ok.OK {
		t.Fatalf("ok.OK false")
	}

	// Ban the client using the special Error code.
	err = sendToDEX("banuser", "{}")
	if err != nil {
		t.Fatalf("banuser send error: %v", err)
	}
	// Just for sequencing
	readChannel(t, "noresponse", banChan)

	msgB := readChannel(t, "banuser msg", recv).([]byte)
	if !strings.Contains(string(msgB), "test quarantine") {
		t.Fatalf("wrong ban message received: %s", string(msgB))
	}

	err = readChannel(t, "banuser err", recv).(error)
	if err == nil {
		t.Fatalf("no read error after ban")
	}

	// Try connecting, and make sure there is an error.
	_, err = newTestDEXClient(address, rootCAs)
	if err == nil {
		t.Fatalf("no websocket connection error after ban")
	}
	// Manually set the ban time.
	server.banMtx.Lock()
	if len(server.quarantine) != 1 {
		t.Fatalf("unexpected number of quarantined IPs")
	}
	for ip := range server.quarantine {
		server.quarantine[ip] = time.Now()
	}
	server.banMtx.Unlock()
	// Now try again. Should connect.
	conn, err := newTestDEXClient(address, rootCAs)
	if err != nil {
		t.Fatalf("error connecting on expired ban")
	}
	var clientCount uint64
	if !giveItASecond(func() bool {
		clientCount = server.clientCount()
		return clientCount == 1
	}) {
		t.Fatalf("server claiming %d clients. Expected 1", clientCount)
	}
	conn.Close()
}

func TestParseListeners(t *testing.T) {
	ipv6wPort := "[fdc5:f621:d3b4:923f::]:80"
	ipv6wZonePort := "[a:b:c:d::%123]:45"
	// Invalid because capital letter O.
	ipv6Invalid := "[1200:0000:AB00:1234:O000:2552:7777:1313]:1234"
	ipv4wPort := "36.182.54.55:80"

	ips := []string{
		ipv6wPort,
		ipv6wZonePort,
		ipv4wPort,
	}

	out4, out6, hasWildcard, err := parseListeners(ips)
	if err != nil {
		t.Fatalf("error parsing listeners: %v", err)
	}
	if len(out4) != 1 {
		t.Fatalf("expected 1 ipv4 addresses. found %d", len(out4))
	}
	if len(out6) != 2 {
		t.Fatalf("expected 2 ipv6 addresses. found %d", len(out6))
	}
	if hasWildcard {
		t.Fatal("hasWildcard true. should be false.")
	}

	// Port-only address goes in both.
	ips = append(ips, ":1234")
	out4, out6, hasWildcard, err = parseListeners(ips)
	if err != nil {
		t.Fatalf("error parsing listeners with wildcard: %v", err)
	}
	if len(out4) != 2 {
		t.Fatalf("expected 2 ipv4 addresses. found %d", len(out4))
	}
	if len(out6) != 3 {
		t.Fatalf("expected 3 ipv6 addresses. found %d", len(out6))
	}
	if !hasWildcard {
		t.Fatal("hasWildcard false with port-only address")
	}

	// No port is invalid
	ips = append(ips, "localhost")
	_, _, _, err = parseListeners(ips)
	if err == nil {
		t.Fatal("no error when no IP specified")
	}

	// Pass invalid address
	_, _, _, err = parseListeners([]string{ipv6Invalid})
	if err == nil {
		t.Fatal("no error with invalid address")
	}
}

type tHTTPHandler struct {
	count uint32
}

func (h *tHTTPHandler) ServeHTTP(http.ResponseWriter, *http.Request) {
	atomic.AddUint32(&h.count, 1)
}

func TestRateLimiter(t *testing.T) {
	tHandler := &tHTTPHandler{}
	s := Server{dataEnabled: 1}

	f := s.limitRate(tHandler)
	ip := "ip"
	req := &http.Request{RemoteAddr: ip}
	recorder := httptest.NewRecorder()
	for i := 0; i < ipMaxBurstSize; i++ {
		f.ServeHTTP(recorder, req)
	}
	time.Sleep(100 * time.Millisecond)
	f.ServeHTTP(recorder, req)
	successes := atomic.LoadUint32(&tHandler.count)
	if successes != ipMaxBurstSize {
		t.Fatalf("expected %d requests. got %d", ipMaxBurstSize, successes)
	}
	statusCode := recorder.Result().StatusCode
	if statusCode != http.StatusTooManyRequests {
		t.Fatalf("wrong status code. wanted %d, got %d", http.StatusTooManyRequests, statusCode)
	}

}
