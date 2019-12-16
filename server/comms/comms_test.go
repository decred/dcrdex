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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/ws"
	"github.com/gorilla/websocket"
)

var testCtx context.Context

func newServer() *Server {
	return &Server{
		clients:    make(map[uint64]*wsLink),
		quarantine: make(map[string]time.Time),
	}
}

type wsConnStub struct {
	msg     chan []byte
	quit    chan struct{}
	write   int
	read    int
	close   int
	lastMsg []byte
}

func newWsStub() *wsConnStub {
	return &wsConnStub{
		msg:  make(chan []byte),
		quit: make(chan struct{}),
	}
}

var testMtx sync.RWMutex

// check results under lock.
func lockedExe(f func()) {
	testMtx.Lock()
	defer testMtx.Unlock()
	f()
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
		return 0, nil, fmt.Errorf("test nonEOF error")
	}
	conn.read++
	return 0, b, nil
}

var writeErr = ""

func (conn *wsConnStub) WriteMessage(msgType int, msg []byte) error {
	testMtx.Lock()
	defer testMtx.Unlock()
	if msgType == websocket.PingMessage {
		select {
		case conn.msg <- pongTrigger:
		default:
		}
		return nil
	}
	conn.lastMsg = msg
	conn.write++
	if writeErr == "" {
		return nil
	}
	err := fmt.Errorf(writeErr)
	writeErr = ""
	return err
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

func sendToConn(t *testing.T, conn *wsConnStub, method, msg string) {
	encMsg, err := json.Marshal(makeReq(method, msg))
	if err != nil {
		t.Fatalf("error encoding %s request: %v", method, err)
	}
	conn.msg <- encMsg
	time.Sleep(time.Millisecond * 10)
}

func sendReplace(t *testing.T, conn *wsConnStub, thing interface{}, old, new string) {
	enc, err := json.Marshal(thing)
	if err != nil {
		t.Fatalf("error encoding thing for sendReplace: %v", err)
	}
	s := string(enc)
	s = strings.ReplaceAll(s, old, new)
	conn.msg <- []byte(s)
	time.Sleep(time.Millisecond)
}

func newTestDEXClient(addr string, rootCAs *x509.CertPool) (*websocket.Conn, error) {
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment, // Same as DefaultDialer.
		HandshakeTimeout: 10 * time.Second,          // DefaultDialer is 45 seconds.
		TLSClientConfig: &tls.Config{
			RootCAs:            rootCAs,
			InsecureSkipVerify: true,
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
	// UseLogger(slog.NewBackend(os.Stdout).Logger("COMMSTEST"))
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
	var client *wsLink
	var conn *wsConnStub
	stubAddr := "testaddr"
	sendToServer := func(method, msg string) { sendToConn(t, conn, method, msg) }

	clientOn := func() bool {
		var on bool
		lockedExe(func() {
			on = !client.Off()
		})
		return on
	}

	// Register all methods before sending any requests.
	// 'getclient' grabs the server's link.
	Route("getclient", func(c Link, _ *msgjson.Message) *msgjson.Error {
		testMtx.Lock()
		defer testMtx.Unlock()
		var ok bool
		client, ok = c.(*wsLink)
		if !ok {
			t.Fatalf("failed to assert client type")
		}
		return nil
	})
	// Check request parses the request to a map of strings.
	var parsedParams map[string]string
	Route("checkrequest", func(c Link, msg *msgjson.Message) *msgjson.Error {
		testMtx.Lock()
		defer testMtx.Unlock()
		parsedParams = make(map[string]string)
		err := json.Unmarshal(msg.Payload, &parsedParams)
		if err != nil {
			t.Fatalf("request parse error: %v", err)
		}
		if client.id != c.ID() {
			t.Fatalf("client ID mismatch. %d != %d", client.id, c.ID())
		}
		return nil
	})
	// 'checkinvalid' should never be run, since the request has invalid
	// formatting.
	passed := false
	Route("checkinvalid", func(_ Link, _ *msgjson.Message) *msgjson.Error {
		testMtx.Lock()
		defer testMtx.Unlock()
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

	// A helper function to reconnect to the server and grab the server's
	// link.
	reconnect := func() {
		conn = newWsStub()
		go server.websocketHandler(conn, stubAddr)
		time.Sleep(time.Millisecond * 10)
		sendToServer("getclient", `{}`)
	}
	reconnect()

	sendToServer("getclient", `{}`)
	lockedExe(func() {
		if client == nil {
			t.Fatalf("'getclient' failed")
		}
		if server.clientCount() != 1 {
			t.Fatalf("clientCount != 1")
		}
	})

	// Check that the request is parsed as expected.
	sendToServer("checkrequest", `{"key":"value"}`)
	lockedExe(func() {
		v, found := parsedParams["key"]
		if !found {
			t.Fatalf("Route key not found")
		}
		if v != "value" {
			t.Fatalf(`expected "value", got %s`, v)
		}
	})

	// Send invalid params, and make sure the server doesn't pass the message. The
	// server will not disconnect the client.
	ensureReplaceFails := func(old, new string) {
		sendReplace(t, conn, makeReq("checkinvalid", old), old, new)
		if passed {
			t.Fatalf("invalid request passed to handler")
		}
	}
	ensureReplaceFails(`{"a":"b"}`, "?")
	if !clientOn() {
		t.Fatalf("client unexpectedly disconnected after invalid message")
	}

	// Send the invalid message again, but error out on the server's WriteMessage
	// attempt. The server should disconnect the client in this case.
	writeErr = "basic error"
	ensureReplaceFails(`{"a":"b"}`, "?")
	if clientOn() {
		t.Fatalf("client connected after WriteMessage error")
	}
	if server.clientCount() != 0 {
		t.Fatalf("clientCount != 0")
	}

	// Shut the client down. Check the on flag.
	reconnect()
	lockedExe(func() { client.Disconnect() })
	time.Sleep(time.Millisecond)
	if clientOn() {
		t.Fatalf("shutdown client has on flag set")
	}
	// Shut down again.
	lockedExe(func() { client.Disconnect() })

	// Reconnect and try shutting down with non-EOF error.
	reconnect()
	nonEOF <- struct{}{}
	time.Sleep(time.Millisecond)
	if clientOn() {
		t.Fatalf("failed to shutdown on non-EOF error")
	}

	// Try a non-existent handler. This should not result in a disconnect.
	reconnect()
	sendToServer("nonexistent", "{}")
	if !clientOn() {
		t.Fatalf("client unexpectedly disconnected after invalid method")
	}

	// Again, but with an WriteMessage error when sending error to client. This
	// should result in a disconnection.
	writeErr = "basic error"
	sendToServer("nonexistent", "{}")
	if clientOn() {
		t.Fatalf("client still connected after WriteMessage error for invalid method")
	}

	// An RPC error. No disconnect.
	reconnect()
	sendToServer("error", "{}")
	if !clientOn() {
		t.Fatalf("client unexpectedly disconnected after rpc error")
	}

	// Return a user quarantine error.
	sendToServer("ban", "{}")
	if clientOn() {
		t.Fatalf("client still connected after quarantine error")
	}
	if !server.isQuarantined(stubAddr) {
		t.Fatalf("server has not marked client as quarantined")
	}
	// A call to Send should return ErrClientDisconnected
	lockedExe(func() {
		if !errors.Is(client.Send(nil), ws.ErrClientDisconnected) {
			t.Fatalf("incorrect error for disconnected client")
		}
	})

	checkParseError := func() {
		lockedExe(func() {
			msg, err := msgjson.DecodeMessage(conn.lastMsg)
			if err != nil {
				t.Fatalf("error decoding last message (%s): %v", string(conn.lastMsg), err)
			}
			resp, err := msg.Response()
			if err != nil {
				t.Fatalf("error decoding response payload: %v", err)
			}
			conn.lastMsg = nil
			if resp.Error == nil || resp.Error.Code != msgjson.RPCParseError {
				t.Fatalf("no error after invalid id")
			}
		})
	}

	// Test an invalid ID.
	reconnect()
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
	stubAddr := "testaddr"

	// Register all methods before sending any requests.
	// 'getclient' grabs the server's link.
	Route("grabclient", func(c Link, _ *msgjson.Message) *msgjson.Error {
		testMtx.Lock()
		defer testMtx.Unlock()
		var ok bool
		client, ok = c.(*wsLink)
		if !ok {
			t.Fatalf("failed to assert client type")
		}
		return nil
	})

	getClient := func() {
		encReq, _ := json.Marshal(makeReq("grabclient", `{}`))
		conn.msg <- encReq
		time.Sleep(time.Millisecond * 10)
	}

	sendToClient := func(route, payload string, f func(Link, *msgjson.Message)) uint64 {
		req := makeReq(route, payload)
		lockedExe(func() {
			err := client.Request(req, f)
			if err != nil {
				t.Logf("sendToClient error: %v", err)
			}
			time.Sleep(time.Millisecond * 10)
		})
		return req.ID
	}

	respondToServer := func(id uint64, msg string) {
		encResp, err := json.Marshal(makeResp(id, msg))
		if err != nil {
			t.Fatalf("error encoding %v (%T) request: %v", id, id, err)
		}
		conn.msg <- encResp
		time.Sleep(time.Millisecond * 10)
	}

	reconnect := func() {
		conn = newWsStub()
		go server.websocketHandler(conn, stubAddr)
		time.Sleep(time.Millisecond * 10)
		getClient()
	}
	reconnect()

	// Send a request from the server to the client, setting a flag when the
	// client responds.
	responded := make(chan struct{}, 1)
	id := sendToClient("looptest", `{}`, func(_ Link, _ *msgjson.Message) {
		responded <- struct{}{}
	})
	// Respond to the server
	respondToServer(id, `{}`)
	select {
	case <-responded:
		// Response received, no problem.
	default:
		// No response falls through.
		t.Fatalf("no response for looptest")
	}

	checkParseError := func(tag string) {
		lockedExe(func() {
			msg, err := msgjson.DecodeMessage(conn.lastMsg)
			if err != nil {
				t.Fatalf("error decoding last message (%s): %v", tag, err)
			}
			resp, err := msg.Response()
			if err != nil {
				t.Fatalf("error decoding response (%s): %v", tag, err)
			}
			conn.lastMsg = nil
			if resp.Error == nil || resp.Error.Code != msgjson.RPCParseError {
				t.Fatalf("no error after %s", tag)
			}
		})
	}

	// Test an invalid id type.
	sendReplace(t, conn, makeResp(1, `{}`), `:1`, `:0`)
	checkParseError("invalid id")

	// Send an invalid payload.
	old := `{"a":"b"}`
	sendReplace(t, conn, makeResp(id, old), old, `?`)
	checkParseError("invalid payload")

	// check the response handler expiration
	lockedExe(func() {
		client.respHandlers = make(map[uint64]*responseHandler)
	})
	id = sendToClient("expiration", `{}`, func(_ Link, _ *msgjson.Message) {})
	// Set the expiration to now
	lockedExe(func() {
		handler, found := client.respHandlers[id]
		if !found {
			t.Fatalf("response handler not found")
		}
		if len(client.respHandlers) != 1 {
			t.Fatalf("expected 1 response handler, found %d", len(client.respHandlers))
		}
		handler.expiration = time.Now()
	})
	// If we send another, the length should still be one, because the expired
	// handler is pruned.
	sendToClient("expiration", `{}`, func(_ Link, _ *msgjson.Message) {})
	lockedExe(func() {
		client.reqMtx.Lock()
		defer client.reqMtx.Unlock()
		if len(client.respHandlers) != 1 {
			t.Fatalf("expired response handler not pruned")
		}
		_, found := client.respHandlers[id]
		if found {
			t.Fatalf("expired response handler still in map")
		}
	})
}

func TestOnline(t *testing.T) {
	portAddr := ":57623"
	address := "wss://127.0.0.1:57623/ws"
	tempDir, err := ioutil.TempDir("", "example")
	if err != nil {
		t.Fatalf("TempDir error: %v", err)
	}
	defer os.RemoveAll(tempDir) // clean up

	keyPath := filepath.Join(tempDir, "rpc.key")
	certPath := filepath.Join(tempDir, "rpc.cert")
	pongWait = 50 * time.Millisecond
	pingPeriod = (pongWait * 9) / 10
	server, err := NewServer(&RPCConfig{
		ListenAddrs: []string{portAddr},
		RPCKey:      keyPath,
		RPCCert:     certPath,
	})
	if err != nil {
		t.Fatalf("server constructor error: %v", err)
	}
	defer server.Stop()

	// Register routes before starting server.
	// No response simulates a route that returns no response.
	Route("noresponse", func(_ Link, _ *msgjson.Message) *msgjson.Error {
		return nil
	})
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
	Route("banuser", func(c Link, req *msgjson.Message) *msgjson.Error {
		rpcErr := msgjson.NewError(msgjson.RPCQuarantineClient, "test quarantine")
		msg, _ := msgjson.NewResponse(req.ID, nil, rpcErr)
		err := c.Send(msg)
		if err != nil {
			t.Fatalf("banuser route send error: %v", err)
		}
		c.Banish()
		return nil
	})

	server.Start()

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

	var response, r []byte
	// Check if the response is nil.
	nilResponse := func() bool {
		var isNil bool
		lockedExe(func() { isNil = response == nil })
		return isNil
	}

	// A loop to grab responses from the server.
	var readErr, re error
	go func() {
		for {
			_, r, re = remoteClient.ReadMessage()
			lockedExe(func() {
				response = r
				readErr = re
			})
			if re != nil {
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
		time.Sleep(time.Millisecond * 10)
		return err
	}

	// Sleep for a few  pongs to make sure the client doesn't disconnect.
	time.Sleep(pongWait * 3)

	err = sendToDEX("noresponse", "{}")
	if err != nil {
		t.Fatalf("noresponse send error: %v", err)
	}
	if !nilResponse() {
		t.Fatalf("response set for 'noresponse' request")
	}

	err = sendToDEX("ok", "{}")
	if err != nil {
		t.Fatalf("noresponse send error: %v", err)
	}
	if nilResponse() {
		t.Fatalf("no response set for 'ok' request")
	}
	var resp *msgjson.ResponsePayload
	lockedExe(func() {
		msg, _ := msgjson.DecodeMessage(response)
		resp, _ = msg.Response()
	})
	ok := new(okresult)
	err = json.Unmarshal(resp.Result, ok)
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
	lockedExe(func() {
		if readErr == nil {
			t.Fatalf("no read error after ban")
		}
	})
	// Try connecting, and make sure there is an error.
	_, err = newTestDEXClient(address, rootCAs)
	if err == nil {
		t.Fatalf("no websocket connection error after ban")
	}
	// Manually set the ban time.
	func() {
		server.banMtx.Lock()
		defer server.banMtx.Unlock()
		if len(server.quarantine) != 1 {
			t.Fatalf("unexpected number of quarantined IPs")
		}
		for ip := range server.quarantine {
			server.quarantine[ip] = time.Now()
		}
	}()
	// Now try again. Should connect.
	conn, err := newTestDEXClient(address, rootCAs)
	if err != nil {
		t.Fatalf("error connecting on expired ban")
	}
	time.Sleep(time.Millisecond * 10)
	clientCount := server.clientCount()
	if clientCount != 1 {
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
