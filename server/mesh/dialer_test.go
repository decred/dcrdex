package mesh

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/db"
	"github.com/gorilla/websocket"
)

type tPeerConn struct {
	id   uint64
	addr string
	done chan struct{}
	once sync.Once
}

var _ peerTransport = (*tPeerConn)(nil)

func newTPeerConn() *tPeerConn {
	return &tPeerConn{
		id:   nextConnID(),
		addr: "test-peer",
		done: make(chan struct{}),
	}
}

func (c *tPeerConn) ID() uint64                  { return c.id }
func (c *tPeerConn) Addr() string                { return c.addr }
func (c *tPeerConn) Send(*msgjson.Message) error { return nil }
func (c *tPeerConn) Request(context.Context, string, any, any) error {
	return nil
}
func (c *tPeerConn) Authorize()            {}
func (c *tPeerConn) Done() <-chan struct{} { return c.done }
func (c *tPeerConn) Disconnect() {
	c.once.Do(func() {
		close(c.done)
	})
}

func (c *tPeerConn) requestHello(context.Context, *helloMessage) (*helloResponse, error) {
	return nil, nil
}
func (c *tPeerConn) requestDecision(context.Context, *decisionMessage) error { return nil }

func TestParsePeerURL(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		want    string
		wantErr bool
	}{
		{name: "host port defaults to tls", addr: "127.0.0.1:17232", want: "wss://127.0.0.1:17232/meshws"},
		{name: "ws no path", addr: "ws://127.0.0.1:17232", want: "ws://127.0.0.1:17232/meshws"},
		{name: "wss with path", addr: "wss://127.0.0.1:17232/custom", want: "wss://127.0.0.1:17232/custom"},
		{name: "wss trailing slash defaults path", addr: "wss://127.0.0.1:17232/", want: "wss://127.0.0.1:17232/meshws"},
		{name: "no scheme trailing slash", addr: "127.0.0.1:17232/", want: "wss://127.0.0.1:17232/meshws"},
		{name: "no scheme with explicit path", addr: "127.0.0.1:17232/custom", want: "wss://127.0.0.1:17232/custom"},
		{name: "no scheme with meshws path", addr: "127.0.0.1:17232/meshws", want: "wss://127.0.0.1:17232/meshws"},
		{name: "surrounding whitespace", addr: "  wss://127.0.0.1:17232  ", want: "wss://127.0.0.1:17232/meshws"},
		{name: "invalid host", addr: "bad host:17232", wantErr: true},
		{name: "missing port", addr: "mesh.example", wantErr: true},
		{name: "invalid port", addr: "mesh.example:notaport", wantErr: true},
		{name: "unsupported scheme", addr: "http://mesh.example:17232", wantErr: true},
		{name: "empty", addr: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePeerURL(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parsePeerURL error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got.String() != tt.want {
				t.Fatalf("parsePeerURL = %q, want %q", got.String(), tt.want)
			}
		})
	}
}

type testDialerNode struct {
	hasPeer        func() bool
	apply          func(context.Context, link, *handshakeResult, string) error
	incompatible   func(error)
	masterEvidence func(time.Time)
}

func (r *testDialerNode) hasPeerConnection() bool {
	if r.hasPeer == nil {
		return false
	}
	return r.hasPeer()
}

func (r *testDialerNode) applyHandshakeResult(ctx context.Context, conn link, result *handshakeResult, initiatorNodeID string) error {
	if r.apply == nil {
		return nil
	}
	return r.apply(ctx, conn, result, initiatorNodeID)
}

func (r *testDialerNode) postDialIncompatible(err error) {
	if r.incompatible != nil {
		r.incompatible(err)
	}
}

func (r *testDialerNode) postMasterEvidence(at time.Time) {
	if r.masterEvidence != nil {
		r.masterEvidence(at)
	}
}

func TestConnectPeer(t *testing.T) {
	// completeParams captures the arguments applyHandshakeResult was invoked with
	// during a TestConnectPeer subtest.
	type completeParams struct {
		peerNodeID      string
		initiatorNodeID string
		role            helloRole
		progress        progressState
		peerFrontier    *db.EventLogPosition
	}

	okHandshake := func(peerNodeID string, role helloRole, prog progressState) handshakeFunc {
		return func(context.Context, handshakeTransport) (*handshakeResult, error) {
			return &handshakeResult{
				peerHello: &helloMessage{
					NodeID:   peerNodeID,
					Role:     role,
					Frontier: toFrontierMessage(&db.EventLogPosition{Seq: 4, TipHash: testTipHash(4)}),
				},
				progress: prog,
			}, nil
		}
	}
	failHandshake := func(err error) handshakeFunc {
		return func(context.Context, handshakeTransport) (*handshakeResult, error) {
			return nil, err
		}
	}

	tlsIncompatErr := wrapPeerIncompatible(x509.UnknownAuthorityError{})
	plainDialErr := errors.New("dial tcp: connection refused")
	handshakeErr := errors.New("handshake rejected")
	completeErr := errors.New("apply failed")

	defaultCompleteParams := &completeParams{
		peerNodeID:      "peer-id",
		initiatorNodeID: "local",
		role:            roleSlave,
		progress:        progressEqual,
		peerFrontier:    &db.EventLogPosition{Seq: 4, TipHash: testTipHash(4)},
	}

	tests := []struct {
		name                    string
		hasPeerConnection       bool
		dialPeer                *tPeerConn
		dialErr                 error
		handshakeFn             handshakeFunc
		applyHandshakeResultErr error

		wantDialCalled           bool
		wantHandshakeFnCalled    bool
		wantCompleteParams       *completeParams // nil means applyHandshakeResult not expected
		wantPostDialIncompatible bool
		wantPostMasterEvidence   bool
		wantConnDisconnected     bool
	}{
		{
			name:              "already connected short-circuits before dial",
			hasPeerConnection: true,
		},
		{
			name:           "plain dial error does not trigger postDialIncompatible",
			dialErr:        plainDialErr,
			wantDialCalled: true,
		},
		{
			name:                     "TLS dial error triggers postDialIncompatible",
			dialErr:                  tlsIncompatErr,
			wantDialCalled:           true,
			wantPostDialIncompatible: true,
		},
		{
			name:                  "handshake failure disconnects conn",
			dialPeer:              newTPeerConn(),
			handshakeFn:           failHandshake(handshakeErr),
			wantDialCalled:        true,
			wantHandshakeFnCalled: true,
			wantConnDisconnected:  true,
		},
		{
			name:                     "handshake incompatibility triggers postDialIncompatible and disconnects",
			dialPeer:                 newTPeerConn(),
			handshakeFn:              failHandshake(wrapPeerIncompatible(errors.New("hello rejected"))),
			wantDialCalled:           true,
			wantHandshakeFnCalled:    true,
			wantPostDialIncompatible: true,
			wantConnDisconnected:     true,
		},
		{
			name:                   "already-connected rejection posts master evidence and disconnects",
			dialPeer:               newTPeerConn(),
			handshakeFn:            failHandshake(fmt.Errorf("mesh hello: %w", errPeerAlreadyConnected)),
			wantDialCalled:         true,
			wantHandshakeFnCalled:  true,
			wantPostMasterEvidence: true,
			wantConnDisconnected:   true,
		},
		{
			name:                  "happy path calls applyHandshakeResult",
			dialPeer:              newTPeerConn(),
			handshakeFn:           okHandshake("peer-id", roleSlave, progressEqual),
			wantDialCalled:        true,
			wantHandshakeFnCalled: true,
			wantCompleteParams:    defaultCompleteParams,
		},
		{
			name:                    "applyHandshakeResult failure disconnects conn",
			dialPeer:                newTPeerConn(),
			handshakeFn:             okHandshake("peer-id", roleSlave, progressEqual),
			applyHandshakeResultErr: completeErr,
			wantDialCalled:          true,
			wantHandshakeFnCalled:   true,
			wantCompleteParams:      defaultCompleteParams,
			wantConnDisconnected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peerURL, err := parsePeerURL("wss://mesh.example:17232")
			if err != nil {
				t.Fatalf("parsePeerURL error: %v", err)
			}

			var (
				dialCalled           bool
				handshakeCalled      bool
				incompatibleCalled   bool
				masterEvidenceCalled bool
				gotComplete          *completeParams
			)

			cm := &outboundDialer{
				peerURL:           peerURL,
				reconnectInterval: time.Second,
				log:               dex.Disabled,
				handshakeSvc:      &handshakeService{nodeID: "local"},
				node: &testDialerNode{
					hasPeer: func() bool { return tt.hasPeerConnection },
					apply: func(_ context.Context, conn link, result *handshakeResult, initiatorNodeID string) error {
						if conn != tt.dialPeer {
							t.Fatalf("applyHandshakeResult conn = %v, want dial peer", conn)
						}
						gotComplete = &completeParams{
							peerNodeID:      result.peerHello.NodeID,
							initiatorNodeID: initiatorNodeID,
							role:            result.peerHello.Role,
							progress:        result.progress,
							peerFrontier:    fromFrontierMessage(result.peerHello.Frontier),
						}
						return tt.applyHandshakeResultErr
					},
					incompatible:   func(error) { incompatibleCalled = true },
					masterEvidence: func(time.Time) { masterEvidenceCalled = true },
				},
			}

			dialFn := func(context.Context) (peerTransport, *sync.WaitGroup, error) {
				dialCalled = true
				if tt.dialPeer != nil {
					return tt.dialPeer, &sync.WaitGroup{}, nil
				}
				return nil, nil, tt.dialErr
			}
			handshakeFn := func(ctx context.Context, conn handshakeTransport) (*handshakeResult, error) {
				handshakeCalled = true
				if tt.handshakeFn == nil {
					t.Fatal("handshakeFn not expected to be called")
					return nil, nil
				}
				return tt.handshakeFn(ctx, conn)
			}

			cm.connectPeer(t.Context(), dialFn, handshakeFn)

			if dialCalled != tt.wantDialCalled {
				t.Errorf("dialCalled = %v, want %v", dialCalled, tt.wantDialCalled)
			}
			if handshakeCalled != tt.wantHandshakeFnCalled {
				t.Errorf("handshakeFnCalled = %v, want %v", handshakeCalled, tt.wantHandshakeFnCalled)
			}
			if !reflect.DeepEqual(gotComplete, tt.wantCompleteParams) {
				t.Errorf("applyHandshakeResult params = %+v, want %+v", gotComplete, tt.wantCompleteParams)
			}
			if incompatibleCalled != tt.wantPostDialIncompatible {
				t.Errorf("postDialIncompatible called = %v, want %v", incompatibleCalled, tt.wantPostDialIncompatible)
			}
			if masterEvidenceCalled != tt.wantPostMasterEvidence {
				t.Errorf("postMasterEvidence called = %v, want %v", masterEvidenceCalled, tt.wantPostMasterEvidence)
			}
			if tt.dialPeer != nil {
				disconnected := false
				select {
				case <-tt.dialPeer.done:
					disconnected = true
				default:
				}
				if disconnected != tt.wantConnDisconnected {
					t.Errorf("connDisconnected = %v, want %v", disconnected, tt.wantConnDisconnected)
				}
			} else if tt.wantConnDisconnected {
				t.Fatal("cannot assert connDisconnected without a dialPeer")
			}
		})
	}
}

// startMeshWSServer starts a minimal httptest server that upgrades /meshws to
// a websocket and blocks reading until the peer disconnects.
func startMeshWSServer(t *testing.T, tls bool) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("/meshws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	if tls {
		return httptest.NewTLSServer(mux)
	}
	return httptest.NewServer(mux)
}

// peerAddrFrom converts an httptest server URL (http:// or https://) into a
// ws:// or wss:// URL pointing at /meshws, letting tests drive the dialer at
// the protocol of their choice regardless of the server's scheme.
func peerAddrFrom(t *testing.T, srvURL, scheme string) string {
	t.Helper()
	u, err := url.Parse(srvURL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	return scheme + "://" + u.Host + "/meshws"
}

func TestDial(t *testing.T) {
	wsSrv := startMeshWSServer(t, false)
	defer wsSrv.Close()

	wssSrv := startMeshWSServer(t, true)
	defer wssSrv.Close()

	wssCertPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: wssSrv.Certificate().Raw,
	})

	tests := []struct {
		name             string
		peerAddr         string
		peerCert         []byte
		wantErr          bool
		wantIncompatible bool
	}{
		{
			name:     "plaintext happy path",
			peerAddr: peerAddrFrom(t, wsSrv.URL, "ws"),
		},
		{
			name:     "tls happy path",
			peerAddr: peerAddrFrom(t, wssSrv.URL, "wss"),
			peerCert: wssCertPEM,
		},
		{
			name:     "wss without peer cert",
			peerAddr: peerAddrFrom(t, wssSrv.URL, "wss"),
			wantErr:  true,
		},
		{
			name:             "wss dialing plaintext peer is incompatible",
			peerAddr:         peerAddrFrom(t, wsSrv.URL, "wss"),
			peerCert:         wssCertPEM,
			wantErr:          true,
			wantIncompatible: true,
		},
		{
			name:             "ws dialing tls peer is a plain dial failure",
			peerAddr:         peerAddrFrom(t, wssSrv.URL, "ws"),
			wantErr:          true,
			wantIncompatible: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm, err := newOutboundDialer(tt.peerAddr, tt.peerCert, dex.Disabled, nil, nil, &testDialerNode{})
			if err != nil {
				if !tt.wantErr {
					t.Fatalf("newOutboundDialer error: %v", err)
				}
				if errors.Is(err, errPeerIncompatible) != tt.wantIncompatible {
					t.Fatalf("errors.Is(%v, errPeerIncompatible) = %v, want %v",
						err, errors.Is(err, errPeerIncompatible), tt.wantIncompatible)
				}
				return
			}

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			conn, wg, err := cm.dial(ctx)
			if (err != nil) != tt.wantErr {
				t.Fatalf("dial err = %v, wantErr %v", err, tt.wantErr)
			}
			if errors.Is(err, errPeerIncompatible) != tt.wantIncompatible {
				t.Fatalf("errors.Is(%v, errPeerIncompatible) = %v, want %v",
					err, errors.Is(err, errPeerIncompatible), tt.wantIncompatible)
			}

			if err == nil {
				if conn == nil {
					t.Fatal("nil conn on successful dial")
				}
				if wg == nil {
					t.Fatal("nil wg on successful dial")
				}
				conn.Disconnect()
				wg.Wait()
			}
		})
	}
}
