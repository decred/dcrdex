package comms

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
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

	"github.com/decred/dcrdex/server/comms/rpc"
	// "github.com/decred/slog"
	"github.com/gorilla/websocket"
)

var testCtx context.Context

func TestMain(m *testing.M) {
	var shutdown func()
	testCtx, shutdown = context.WithCancel(context.Background())
	defer shutdown()
	// UseLogger(slog.NewBackend(os.Stdout).Logger("COMMSTEST"))
	os.Exit(m.Run())
}

func newServer() *RPCServer {
	return &RPCServer{
		clients:    make(map[uint64]*RPCClient),
		quarantine: make(map[string]time.Time),
	}
}

type wsConnStub struct {
	msg   chan []byte
	quit  chan struct{}
	write int
	read  int
	close int
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

func (conn *wsConnStub) ReadMessage() (int, []byte, error) {
	var b []byte
	select {
	case b = <-conn.msg:
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

func (conn *wsConnStub) WriteMessage(int, []byte) error {
	testMtx.Lock()
	defer testMtx.Unlock()
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

func dummyRPCMethod(_ *RPCClient, _ *rpc.Request) *rpc.RPCError {
	return nil
}

var reqID int

func makeReq(method, msg string) *rpc.Request {
	reqID++
	return &rpc.Request{
		Jsonrpc: rpc.JSONRPCVersion,
		Method:  method,
		Params:  []byte(msg),
		ID:      reqID,
	}
}

// method strings cannot be empty.
func TestRegisterMethod_PanicsEmtpyString(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("no panic on registering empty string method")
		}
	}()
	RegisterMethod("", dummyRPCMethod)
}

// methods cannot be registered more than once.
func TestRegisterMethod_PanicsDoubleRegistry(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("no panic on registering empty string method")
		}
	}()
	RegisterMethod("somemethod", dummyRPCMethod)
	RegisterMethod("somemethod", dummyRPCMethod)
}

// Test the server with a stub for the client connections.
func TestOffline(t *testing.T) {
	server := newServer()
	var client *RPCClient
	var conn *wsConnStub
	stubAddr := "testaddr"

	sendReq := func(method, msg string) {
		encodedRequest, err := json.Marshal(makeReq(method, msg))
		if err != nil {
			t.Fatalf("error encoding %s request: %v", method, err)
		}
		conn.msg <- encodedRequest
		time.Sleep(time.Millisecond * 10)
	}

	clientOn := func() bool {
		var on bool
		lockedExe(func() {
			client.quitMtx.RLock()
			defer client.quitMtx.RUnlock()
			on = client.on
		})
		return on
	}

	// Register all methods before sending any requests.
	// 'getclient' grabs the server's RPCClient.
	RegisterMethod("getclient", func(c *RPCClient, _ *rpc.Request) *rpc.RPCError {
		testMtx.Lock()
		defer testMtx.Unlock()
		client = c
		return nil
	})
	// Check request parses the request to a map of strings.
	var parsedParams map[string]string
	RegisterMethod("checkrequest", func(c *RPCClient, req *rpc.Request) *rpc.RPCError {
		testMtx.Lock()
		defer testMtx.Unlock()
		parsedParams = make(map[string]string)
		err := json.Unmarshal(req.Params, &parsedParams)
		if err != nil {
			t.Fatalf("request parse error: %v", err)
		}
		if client.id != c.id {
			t.Fatalf("client ID mismatch. %d != %d", client.id, c.id)
		}
		return nil
	})
	// 'checkinvalid' should never be run, since the request has invalid formatting.
	passed := false
	RegisterMethod("checkinvalid", func(_ *RPCClient, _ *rpc.Request) *rpc.RPCError {
		testMtx.Lock()
		defer testMtx.Unlock()
		passed = true
		return nil
	})
	// 'error' returns an RPCError.
	RegisterMethod("error", func(_ *RPCClient, _ *rpc.Request) *rpc.RPCError {
		return &rpc.RPCError{
			Code:    550,
			Message: "somemessage",
		}
	})
	// 'ban' quarantines the user.
	RegisterMethod("ban", func(_ *RPCClient, _ *rpc.Request) *rpc.RPCError {
		return &rpc.RPCError{
			Code:    rpc.RPCQuarantineClient,
			Message: "user quarantined",
		}
	})

	// A helper function to reconnect to the server and grab the server's
	// RPCClient.
	reconnect := func() {
		conn = newWsStub()
		go server.websocketHandler(conn, stubAddr)
		time.Sleep(time.Millisecond * 10)
		sendReq("getclient", `{}`)
	}
	reconnect()

	sendReq("getclient", `{}`)
	lockedExe(func() {
		if client == nil {
			t.Fatalf("'getclient' failed")
		}
		if server.clientCount() != 1 {
			t.Fatalf("clientCount != 1")
		}
	})

	// Check that the request is parsed as expected.
	sendReq("checkrequest", `{"key":"value"}`)
	lockedExe(func() {
		v, found := parsedParams["key"]
		if !found {
			t.Fatalf("RegisterMethod key not found")
		}
		if v != "value" {
			t.Fatalf(`expected "value", got %s`, v)
		}
	})

	// Send invalid params, and make sure the server doesn't pass the message. The
	// server will not disconnect the client.
	sendReplace := func(old, new string) {
		enc, err := json.Marshal(makeReq("checkinvalid", old))
		if err != nil {
			t.Fatalf("error encoding invalid request with old: %v", err)
		}
		s := string(enc)
		s = strings.ReplaceAll(s, old, new)
		conn.msg <- []byte(s)
		time.Sleep(time.Millisecond * 1)
		if passed {
			t.Fatalf("invalid request passed to handler")
		}
	}
	sendReplace(`{"a":"b"}`, "?")
	if !clientOn() {
		t.Fatalf("client unexpectedly disconnected after invalid message")
	}

	// Send the invalid message again, but error out on the server's WriteMessage
	// attempt. The server should disconnect the client in this case.
	writeErr = "basic error"
	sendReplace(`{"a":"b"}`, "?")
	if clientOn() {
		t.Fatalf("client connected after WriteMessage error")
	}
	if server.clientCount() != 0 {
		t.Fatalf("clientCount != 0")
	}

	// Send incorrect RPC version
	reconnect()
	sendReplace(`"2.0"`, `"3.0"`)
	if clientOn() {
		t.Fatalf("client connected after invalid json version")
	}

	// Shut the client down. Check the on flag.
	reconnect()
	lockedExe(func() { client.disconnect() })
	time.Sleep(time.Millisecond * 1)
	if clientOn() {
		t.Fatalf("shutdown client has on flag set")
	}
	// Shut down again.
	lockedExe(func() { client.disconnect() })

	// Reconnect and try shutting down with non-EOF error.
	reconnect()
	nonEOF <- struct{}{}
	time.Sleep(time.Millisecond * 1)
	if clientOn() {
		t.Fatalf("failed to shutdown on non-EOF error")
	}

	// Try a non-existent handler. This should not result in a disconnect.
	reconnect()
	sendReq("nonexistent", "{}")
	if !clientOn() {
		t.Fatalf("client unexpectedly disconnected after invalid method")
	}

	// Again, but with an WriteMessage error when sending error to client. This
	// should result in a disconnection.
	writeErr = "basic error"
	sendReq("nonexistent", "{}")
	if clientOn() {
		t.Fatalf("client still connected after WriteMessage error for invalid method")
	}

	// An RPC error. No disconnect.
	reconnect()
	sendReq("error", "{}")
	if !clientOn() {
		t.Fatalf("client unexpectedly disconnected after rpc error")
	}

	// Return a user quarantine error.
	sendReq("ban", "{}")
	if clientOn() {
		t.Fatalf("client still connected after quarantine error")
	}
	if !server.isQuarantined(stubAddr) {
		t.Fatalf("server has not marked client as quarantined")
	}
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
	server, err := NewRPCServer(&RPCConfig{
		ListenAddrs: []string{portAddr},
		RPCKey:      keyPath,
		RPCCert:     certPath,
	})
	if err != nil {
		t.Fatalf("server constructor error: %v", err)
	}
	defer server.Stop()

	// Register methods before starting server.
	// No response simulates a method that returns no response.
	RegisterMethod("noresponse", func(_ *RPCClient, _ *rpc.Request) *rpc.RPCError {
		return nil
	})
	// The 'ok' method returns an affirmative response.
	type okresult struct {
		OK bool `json:"ok"`
	}
	RegisterMethod("ok", func(c *RPCClient, req *rpc.Request) *rpc.RPCError {
		resp, err := rpc.NewResponse(req.ID, &okresult{OK: true}, nil)
		if err != nil {
			return &rpc.RPCError{
				Code:    500,
				Message: err.Error(),
			}
		}
		err = c.SendMessage(resp)
		if err != nil {
			return &rpc.RPCError{
				Code:    500,
				Message: err.Error(),
			}
		}
		return nil
	})
	// The 'banuser' method quarantines the user.
	RegisterMethod("banuser", func(c *RPCClient, req *rpc.Request) *rpc.RPCError {
		return &rpc.RPCError{
			Code:    rpc.RPCQuarantineClient,
			Message: "test quarantine",
		}
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
	nilResponse := func() bool {
		var isNil bool
		lockedExe(func() { isNil = response == nil })
		return isNil
	}
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

	sendReq := func(method, msg string) error {
		b, err := json.Marshal(makeReq(method, msg))
		if err != nil {
			t.Fatalf("error encoding %s request: %v", method, err)
		}
		err = remoteClient.WriteMessage(websocket.TextMessage, b)
		time.Sleep(time.Millisecond * 10)
		return err
	}

	err = sendReq("noresponse", "{}")
	if err != nil {
		t.Fatalf("noresponse send error: %v", err)
	}
	if !nilResponse() {
		t.Fatalf("response set for 'noresponse' request")
	}

	err = sendReq("ok", "{}")
	if err != nil {
		t.Fatalf("noresponse send error: %v", err)
	}
	if nilResponse() {
		t.Fatalf("no response set for 'ok' request")
	}
	res := new(rpc.Response)
	lockedExe(func() { err = json.Unmarshal(response, &res) })
	if err != nil {
		t.Fatalf("'ok' response unmarshal error: %v", err)
	}
	ok := new(okresult)
	err = json.Unmarshal(res.Result, &ok)
	if err != nil {
		t.Fatalf("'ok' response unmarshal error: %v", err)
	}
	if !ok.OK {
		t.Fatalf("ok.OK false")
	}

	// Now ban the client
	err = sendReq("banuser", "{}")
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
