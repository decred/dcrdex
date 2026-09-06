// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/gorilla/websocket"
)

const defaultReconnectInterval = 5 * time.Second

// peerTransport is the transport pieces needed by the outbound dialer.
type peerTransport interface {
	link
	handshakeTransport
}

// dialFunc dials a peer connection and returns its read-loop waitgroup.
type dialFunc func(ctx context.Context) (peerTransport, *sync.WaitGroup, error)

// handshakeFunc runs the outbound handshake on a connected peer.
type handshakeFunc func(ctx context.Context, conn handshakeTransport) (*handshakeResult, error)

// dialerNode is the node surface the outbound dialer needs.
type dialerNode interface {
	hasPeerConnection() bool
	applyHandshakeResult(ctx context.Context, conn link, result *handshakeResult, initiatorNodeID string) error
	postDialIncompatible(err error)
	postMasterEvidence(at time.Time)
}

// outboundDialer dials the configured peer whenever this node has no peer
// connection. A completed handshake is handed to the control loop, which
// decides whether to adopt the connection.
type outboundDialer struct {
	peerURL           *url.URL
	peerRoots         *x509.CertPool
	reconnectInterval time.Duration
	log               dex.Logger
	handshakeSvc      *handshakeService
	meshRoutes        map[string]meshRoute
	node              dialerNode

	attempts atomic.Uint64

	lastDialMtx sync.Mutex
	lastDialErr string
	lastDialAt  time.Time
}

func newOutboundDialer(
	peerAddr string,
	peerCert []byte,
	log dex.Logger,
	handshakeSvc *handshakeService,
	meshRoutes map[string]meshRoute,
	node dialerNode,
) (*outboundDialer, error) {
	peerURL, err := parsePeerURL(peerAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid peer address: %w", err)
	}

	var peerRoots *x509.CertPool
	if peerURL.Scheme == "wss" {
		if len(peerCert) == 0 {
			return nil, fmt.Errorf("peer certificate required for TLS mesh peer")
		}
		peerRoots = x509.NewCertPool()
		if ok := peerRoots.AppendCertsFromPEM(peerCert); !ok {
			return nil, fmt.Errorf("invalid peer cert")
		}
	}

	return &outboundDialer{
		peerURL:           peerURL,
		peerRoots:         peerRoots,
		reconnectInterval: defaultReconnectInterval,
		log:               log,
		handshakeSvc:      handshakeSvc,
		meshRoutes:        meshRoutes,
		node:              node,
	}, nil
}

// attemptCount reports outbound dial attempts since startup.
func (d *outboundDialer) attemptCount() uint64 {
	return d.attempts.Load()
}

// lastDialError reports the most recent failed dial/handshake error and when it
// occurred.
func (d *outboundDialer) lastDialError() (string, time.Time) {
	d.lastDialMtx.Lock()
	defer d.lastDialMtx.Unlock()
	return d.lastDialErr, d.lastDialAt
}

// useTLS reports whether the outbound connection uses TLS.
func (d *outboundDialer) useTLS() bool {
	return d.peerURL.Scheme == "wss"
}

// Run runs the reconnect loop until ctx is canceled.
func (d *outboundDialer) Run(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			d.connectPeer(ctx, d.dial, d.handshakeSvc.initiateHandshake)
			timer.Reset(d.reconnectInterval)
		}
	}
}

// connectPeer makes one connection attempt, unless the node already has a
// peer connection. dial and initiateHandshake are parameters so the flow can
// be tested without websockets.
func (d *outboundDialer) connectPeer(ctx context.Context, dial dialFunc, initiateHandshake handshakeFunc) {
	if d.node.hasPeerConnection() {
		return
	}

	d.attempts.Add(1)

	err := d.dialAndHandshake(ctx, dial, initiateHandshake)
	if err == nil || ctx.Err() != nil {
		return
	}

	d.lastDialMtx.Lock()
	repeatErr := d.lastDialErr == err.Error()
	d.lastDialErr = err.Error()
	d.lastDialAt = time.Now()
	d.lastDialMtx.Unlock()

	// The signal is posted on every attempt because master evidence must stay
	// fresh, but avoid spamming the logs.
	var what string
	switch {
	case errors.Is(err, errPeerAlreadyConnected):
		what = "still holds the active session; postponing slave promotion"
		d.node.postMasterEvidence(time.Now())
	case errors.Is(err, errPeerIncompatible):
		what = "is incompatible"
		d.node.postDialIncompatible(err)
	default:
		what = "connection ended"
	}
	if repeatErr {
		d.log.Debugf("Mesh peer %s %s: %v", d.peerURL, what, err)
	} else {
		d.log.Warnf("Mesh peer %s %s: %v", d.peerURL, what, err)
	}
}

// dialAndHandshake dials the peer, runs the outbound handshake, and hands the
// result to the control loop via applyHandshakeResult.
func (d *outboundDialer) dialAndHandshake(ctx context.Context, dial dialFunc, initiateHandshake handshakeFunc) error {
	conn, wg, err := dial(ctx)
	if err != nil {
		return err
	}

	handshake, err := initiateHandshake(ctx, conn)
	if err == nil {
		conn.Authorize()
		err = d.node.applyHandshakeResult(ctx, conn, handshake, d.handshakeSvc.nodeID)
	}
	if err != nil {
		conn.Disconnect()
		wg.Wait()
		return err
	}

	d.log.Infof("Connected to mesh peer %s", d.peerURL)
	return nil
}

// dial opens a websocket connection to the configured peer and wraps it in an
// rpcConn. It is the production implementation of dialFunc used by Run.
func (d *outboundDialer) dial(ctx context.Context) (peerTransport, *sync.WaitGroup, error) {
	var tlsConfig *tls.Config
	if d.useTLS() {
		tlsConfig = &tls.Config{
			RootCAs:    d.peerRoots,
			MinVersion: tls.VersionTLS12,
			ServerName: d.peerURL.Hostname(),
		}
	}

	dialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig:  tlsConfig,
	}
	peerAddr := d.peerURL.String()
	wsConn, _, err := dialer.DialContext(ctx, peerAddr, nil)
	if err != nil {
		if isTLSConfigError(err) {
			err = wrapPeerIncompatible(err)
		}
		return nil, nil, err
	}

	wsConn.SetPongHandler(func(string) error {
		return wsConn.SetReadDeadline(time.Now().Add(meshPingPeriod * 2))
	})

	conn := newRPCConn(ctx, peerAddr, wsConn, d.meshRoutes, d.log)
	wg, err := conn.connect()
	if err != nil {
		return nil, nil, err
	}

	return conn, wg, nil
}

// isTLSConfigError reports whether err is a TLS failure that indicates a
// configuration problem rather than a transient outage.
func isTLSConfigError(err error) bool {
	var recErr tls.RecordHeaderError
	var unknownAuth x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var invalidCert x509.CertificateInvalidError
	var tlsVerify *tls.CertificateVerificationError
	return errors.As(err, &recErr) || errors.As(err, &unknownAuth) ||
		errors.As(err, &hostnameErr) || errors.As(err, &invalidCert) ||
		errors.As(err, &tlsVerify)
}

// parsePeerURL validates and normalizes peerAddr into a *url.URL. A bare
// host:port is treated as wss:// and the path is defaulted to meshWSPath if
// it is empty or the root.
func parsePeerURL(peerAddr string) (*url.URL, error) {
	peerAddr = strings.TrimSpace(peerAddr)
	if peerAddr == "" {
		return nil, fmt.Errorf("empty peer address")
	}

	raw := peerAddr
	if !strings.Contains(raw, "://") {
		raw = "wss://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("missing host")
	}
	port := u.Port()
	if port == "" {
		return nil, fmt.Errorf("missing port")
	}
	if portNum, err := strconv.Atoi(port); err != nil || portNum < 1 || portNum > 65535 {
		return nil, fmt.Errorf("invalid port %q", port)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = meshWSPath
	}

	return u, nil
}

// hasPeerConnection reports if the control loop has a peer connection.
func (n *node) hasPeerConnection() bool {
	return n.control.hasPeerConnection()
}

// postDialIncompatible sends a signal to the control loop to indicate
// the node is incompatible with the peer.
func (n *node) postDialIncompatible(err error) {
	_ = n.control.post(dialIncompatibleSignal{err: err, at: time.Now()})
}

// postMasterEvidence sends a signal to the control loop to indicate
// the serving master is alive, even though it is not the current
// active connection.
func (n *node) postMasterEvidence(at time.Time) {
	_ = n.control.post(masterEvidenceSignal{at: at})
}
