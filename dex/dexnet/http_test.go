package dexnet

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestErrorParsing(t *testing.T) {
	ctx := t.Context()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code": -150, "msg": "you messed up, bruh"}`, http.StatusBadRequest)
	}))
	defer ts.Close()

	var errPayload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := Get(ctx, ts.URL, nil, WithErrorParsing(&errPayload)); err == nil {
		t.Fatal("didn't get an http error")
	}
	if errPayload.Code != -150 || errPayload.Msg != "you messed up, bruh" {
		t.Fatal("unexpected error body")
	}
}

func TestProxyDialContext(t *testing.T) {
	if fn := ProxyDialContext(""); fn != nil {
		t.Fatal("expected nil for empty address")
	}
	if fn := ProxyDialContext("127.0.0.1:1080"); fn == nil {
		t.Fatal("expected non-nil for valid address")
	}
}

func TestProxyTransport(t *testing.T) {
	tr := ProxyTransport("127.0.0.1:1080")
	if tr.DialContext == nil {
		t.Fatal("expected non-nil DialContext")
	}
	if tr.TLSHandshakeTimeout != 10*time.Second {
		t.Fatalf("unexpected TLSHandshakeTimeout: %v", tr.TLSHandshakeTimeout)
	}
	if tr.IdleConnTimeout != 90*time.Second {
		t.Fatalf("unexpected IdleConnTimeout: %v", tr.IdleConnTimeout)
	}
}

func TestProxyHTTPClient(t *testing.T) {
	cl := ProxyHTTPClient("127.0.0.1:1080")
	if cl.Timeout != 20*time.Second {
		t.Fatalf("unexpected timeout: %v", cl.Timeout)
	}
	tr, ok := cl.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected *http.Transport")
	}
	if tr.DialContext == nil {
		t.Fatal("expected non-nil DialContext on transport")
	}
}

// startTestSOCKS5 starts a minimal SOCKS5 server (no-auth, CONNECT only) on a
// random localhost port. It returns the listener address and a cleanup function.
func startTestSOCKS5(t *testing.T) (addr string, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				handleSOCKS5(conn)
			}()
		}
	}()

	return ln.Addr().String(), func() {
		ln.Close()
		wg.Wait()
	}
}

func handleSOCKS5(conn net.Conn) {
	// Greeting: ver | nmethods | methods...
	var hdr [2]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return
	}
	if hdr[0] != 0x05 {
		return
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return
	}
	// Reply: no-auth
	conn.Write([]byte{0x05, 0x00})

	// Connect request: ver | cmd | rsv | atyp | addr | port
	var req [4]byte
	if _, err := io.ReadFull(conn, req[:]); err != nil {
		return
	}
	if req[1] != 0x01 { // CONNECT
		return
	}

	var targetHost string
	switch req[3] { // atyp
	case 0x01: // IPv4
		var ip [4]byte
		if _, err := io.ReadFull(conn, ip[:]); err != nil {
			return
		}
		targetHost = net.IP(ip[:]).String()
	case 0x03: // Domain
		var domLen [1]byte
		if _, err := io.ReadFull(conn, domLen[:]); err != nil {
			return
		}
		dom := make([]byte, domLen[0])
		if _, err := io.ReadFull(conn, dom); err != nil {
			return
		}
		targetHost = string(dom)
	default:
		return
	}

	var portBuf [2]byte
	if _, err := io.ReadFull(conn, portBuf[:]); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(portBuf[:])
	target := net.JoinHostPort(targetHost, strconv.Itoa(int(port)))

	remote, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		// Send failure reply.
		conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()

	// Success reply.
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})

	// Bidirectional copy. Close write halves so the other io.Copy unblocks.
	done := make(chan struct{})
	go func() {
		io.Copy(remote, conn)
		if tc, ok := remote.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
		close(done)
	}()
	io.Copy(conn, remote)
	if tc, ok := conn.(*net.TCPConn); ok {
		tc.CloseWrite()
	}
	<-done
}

func TestProxyHTTPClientRouting(t *testing.T) {
	proxyAddr, cleanup := startTestSOCKS5(t)
	defer cleanup()

	const want = "hello through socks5"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, want)
	}))
	defer ts.Close()

	cl := ProxyHTTPClient(proxyAddr)
	resp, err := cl.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET through SOCKS5 proxy failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(body) != want {
		t.Fatalf("unexpected body: %q, want %q", body, want)
	}
}
