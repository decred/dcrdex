// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package comms

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"github.com/gorilla/websocket"
)

const (
	// bufferSize is buffer size for a websocket connection's read channel.
	readBuffSize = 128

	// The maximum time in seconds to write to a connection.
	writeWait = time.Second * 3

	// reconnectInterval is the initial and increment between reconnect tries.
	reconnectInterval = 5 * time.Second

	// maxReconnectInterval is the maximum allowed reconnect interval.
	maxReconnectInterval = time.Minute

	// DefaultResponseTimeout is the default timeout for responses after a
	// request is successfully sent.
	DefaultResponseTimeout = time.Minute
)

// ConnectionStatus represents the current status of the websocket connection.
type ConnectionStatus uint32

const (
	Disconnected ConnectionStatus = iota
	Connected
	InvalidCert
)

// String gives a human readable string for each connection status.
func (cs ConnectionStatus) String() string {
	switch cs {
	case Disconnected:
		return "disconnected"
	case Connected:
		return "connected"
	case InvalidCert:
		return "invalid certificate"
	default:
		return "unknown status"
	}
}

// invalidCertRegexp is a regexp that helps check for non-typed x509 errors
// caused by or related to an invalid cert.
var invalidCertRegexp = regexp.MustCompile(".*(unknown authority|not standards compliant|not trusted)")

// isErrorInvalidCert checks if the provided error is one of the different
// variant of an invalid cert error returned from the x509 package.
func isErrorInvalidCert(err error) bool {
	var invalidCertErr x509.CertificateInvalidError
	var unknownCertAuthErr x509.UnknownAuthorityError
	var hostNameErr x509.HostnameError
	return errors.As(err, &invalidCertErr) || errors.As(err, &hostNameErr) ||
		errors.As(err, &unknownCertAuthErr) || invalidCertRegexp.MatchString(err.Error())
}

// ErrInvalidCert is the error returned when attempting to use an invalid cert
// to set up a ws connection.
var ErrInvalidCert = fmt.Errorf("invalid certificate")

// ErrCertRequired is the error returned when a ws connection fails because no
// cert was provided.
var ErrCertRequired = fmt.Errorf("certificate required")

type wsConnCommon interface {
	NextID() uint64
	IsDown() bool
	Send(msg *msgjson.Message) error
	SendRaw(b []byte) error
	Request(msg *msgjson.Message, respHandler func(*msgjson.Message)) error
	RequestRaw(msgID uint64, rawMsg []byte, respHandler func(*msgjson.Message)) error
	RequestWithTimeout(msg *msgjson.Message, respHandler func(*msgjson.Message), expireTime time.Duration, expire func()) error
	Connect(ctx context.Context) (*sync.WaitGroup, error)
	MessageSource() <-chan *msgjson.Message
}

// WsConn is a single-endpoint websocket client. The endpoint-specific fields in
// WsCfg are used to create the connection, and UpdateURL can replace that
// single endpoint while preserving the endpoint's certificate and dialer.
type WsConn interface {
	wsConnCommon
	UpdateURL(string)
}

// FailoverWsConn is a websocket client that can rotate among configured
// endpoints on reconnect. Use NewFailoverWsConn with endpoint-specific settings
// in the []*WsEndpoint argument, and SetFailoverEndpoints to update that list.
type FailoverWsConn interface {
	wsConnCommon
	SetFailoverEndpoints([]*WsEndpoint) error
	ActiveEndpoint() string
}

// WsEndpoint is a websocket endpoint and its endpoint-specific connection
// settings.
type WsEndpoint struct {
	URL            string
	Cert           []byte
	NetDialContext func(context.Context, string, string) (net.Conn, error)
}

// When the DEX sends a request to the client, a responseHandler is created
// to wait for the response.
type responseHandler struct {
	expiration *time.Timer
	f          func(*msgjson.Message)
	abort      func() // only to be run at most once, and not if f ran
}

// WsCfg configures websocket behavior common to both WsConn and FailoverWsConn.
// NewWsConn also uses URL, Cert, and NetDialContext as its single endpoint's
// settings. NewFailoverWsConn takes endpoints separately, so those
// endpoint-specific fields must be left unset for failover connections.
type WsCfg struct {
	// URL is the websocket endpoint URL.
	URL string

	// The maximum time in seconds to wait for a ping from the server. This
	// should be larger than the server's ping interval to allow for network
	// latency.
	PingWait time.Duration

	// The server's certificate.
	Cert []byte

	// ReconnectSync runs the needed reconnection synchronization after
	// a reconnect.
	ReconnectSync func()

	// ConnectEventFunc runs whenever connection status changes.
	//
	// NOTE: Disconnect event notifications may lag behind actual
	// disconnections.
	ConnectEventFunc func(ConnectionStatus)

	// Logger is the logger for the WsConn.
	Logger dex.Logger

	// NetDialContext specifies an optional dialer context to use.
	NetDialContext func(context.Context, string, string) (net.Conn, error)

	// RawHandler overrides the msgjson parsing and forwards all messages to
	// the provided function.
	RawHandler func([]byte)

	// DisableAutoReconnect disables automatic reconnection.
	DisableAutoReconnect bool

	ConnectHeaders http.Header

	// EchoPingData will echo any data from pings as the pong data.
	EchoPingData bool
}

// wsConn represents a client websocket connection.
type wsConn struct {
	// 64-bit atomic variables first. See
	// https://golang.org/pkg/sync/atomic/#pkg-note-BUG.
	rID    uint64
	cancel context.CancelFunc
	wg     sync.WaitGroup
	log    dex.Logger
	cfg    *WsCfg
	readCh chan *msgjson.Message

	endpoints *endpointSet
	// connectedURL is the live websocket URL reported by ActiveEndpoint, not
	// the endpoint-set rotation cursor.
	connectedURL atomic.Value // string

	wsMtx    sync.Mutex
	writeMtx sync.Mutex
	ws       *websocket.Conn

	connectionStatus uint32 // atomic

	reqMtx       sync.RWMutex
	respHandlers map[uint64]*responseHandler

	reconnectCh chan struct{} // trigger for immediate reconnect
}

var _ WsConn = (*wsConn)(nil)
var _ FailoverWsConn = (*wsConn)(nil)

// NewWsConn creates a single-endpoint client websocket connection.
func NewWsConn(cfg *WsCfg) (WsConn, error) {
	endpoint := &WsEndpoint{
		URL:            cfg.URL,
		Cert:           cfg.Cert,
		NetDialContext: cfg.NetDialContext,
	}
	return newWsConn(cfg, []*WsEndpoint{endpoint})
}

// NewFailoverWsConn creates a websocket connection that can rotate among
// multiple endpoints on reconnect. The endpoints list must be non-empty, valid,
// and contain no duplicate URLs. Endpoint-specific WsCfg fields, URL, Cert, and
// NetDialContext, must be zero because failover endpoint settings are supplied
// by the endpoints argument.
func NewFailoverWsConn(cfg *WsCfg, endpoints []*WsEndpoint) (FailoverWsConn, error) {
	if cfg.URL != "" {
		return nil, fmt.Errorf("URL must be provided by failover endpoints, not WsCfg")
	}
	if len(cfg.Cert) > 0 {
		return nil, fmt.Errorf("Cert must be provided by failover endpoints, not WsCfg")
	}
	if cfg.NetDialContext != nil {
		return nil, fmt.Errorf("NetDialContext must be provided by failover endpoints, not WsCfg")
	}

	commonCfg := *cfg
	return newWsConn(&commonCfg, endpoints)
}

func newWsConn(cfg *WsCfg, endpointCfgs []*WsEndpoint) (*wsConn, error) {
	if cfg.PingWait < 0 {
		return nil, fmt.Errorf("ping wait cannot be negative")
	}

	endpoints, err := prepareEndpoints(endpointCfgs)
	if err != nil {
		return nil, err
	}

	conn := &wsConn{
		cfg:          cfg,
		log:          cfg.Logger,
		endpoints:    &endpointSet{endpoints: endpoints},
		readCh:       make(chan *msgjson.Message, readBuffSize),
		respHandlers: make(map[uint64]*responseHandler),
		reconnectCh:  make(chan struct{}, 1),
	}
	if conn.log == nil {
		conn.log = dex.Disabled
	}

	return conn, nil
}

// UpdateURL replaces the connection's endpoint set with a single endpoint
// using the selected endpoint's certificate and dialer. It is intended for
// single-endpoint connections whose websocket URL changes.
func (conn *wsConn) UpdateURL(uri string) {
	cfg := conn.endpoints.selectedUpdateCfg(uri)

	endpoint, err := newWsEndpoint(cfg)
	if err != nil {
		conn.log.Warnf("Ignoring invalid websocket URL update %q: %v", uri, err)
		return
	}

	conn.endpoints.replaceWithSingle(endpoint)
}

// SetFailoverEndpoints atomically replaces the failover endpoint list. The list
// must be non-empty, valid, and contain no duplicate URLs. If the selected
// endpoint URL is present in the new list, the next connection attempt uses its
// successor; otherwise the next attempt starts with the first endpoint.
// The current websocket is not reconnected immediately.
func (conn *wsConn) SetFailoverEndpoints(cfgs []*WsEndpoint) error {
	return conn.endpoints.replace(cfgs)
}

// ActiveEndpoint returns the live websocket URL, or empty when down. The URL
// may be absent from the configured list if SetFailoverEndpoints replaced it
// while connected.
func (conn *wsConn) ActiveEndpoint() string {
	if conn.IsDown() {
		return ""
	}
	url, _ := conn.connectedURL.Load().(string)
	return url
}

// IsDown indicates if the connection is known to be down.
func (conn *wsConn) IsDown() bool {
	return atomic.LoadUint32(&conn.connectionStatus) != uint32(Connected)
}

// setConnectionStatus updates the connection's status and runs the
// ConnectEventFunc in case of a change.
func (conn *wsConn) setConnectionStatus(status ConnectionStatus) {
	oldStatus := atomic.SwapUint32(&conn.connectionStatus, uint32(status))
	statusChange := oldStatus != uint32(status)
	if statusChange && conn.cfg.ConnectEventFunc != nil {
		conn.cfg.ConnectEventFunc(status)
	}
}

// connect attempts to establish a websocket connection.
func (conn *wsConn) connect(ctx context.Context) error {
	endpoint := conn.endpoints.nextEndpoint()
	conn.log.Infof("Connecting to websocket endpoint %s...", endpoint.url)
	dialer := &websocket.Dialer{
		HandshakeTimeout: DefaultResponseTimeout,
		TLSClientConfig:  endpoint.tlsCfg,
	}
	if endpoint.netDialContext != nil {
		dialer.NetDialContext = endpoint.netDialContext
	} else {
		dialer.Proxy = http.ProxyFromEnvironment
	}

	ws, _, err := dialer.DialContext(ctx, endpoint.url, conn.cfg.ConnectHeaders)
	if err != nil {
		if isErrorInvalidCert(err) {
			return conn.certConnectError(endpoint, err)
		}
		conn.setConnectionStatus(Disconnected)
		return err
	}

	// Set the initial read deadline for the first ping. Subsequent read
	// deadlines are set in the ping handler.
	err = ws.SetReadDeadline(time.Now().Add(conn.cfg.PingWait))
	if err != nil {
		conn.log.Errorf("set read deadline failed: %v", err)
		return err
	}

	echoPing := conn.cfg.EchoPingData
	ws.SetPingHandler(func(appData string) error {
		now := time.Now()

		// Set the deadline for the next ping.
		err := ws.SetReadDeadline(now.Add(conn.cfg.PingWait))
		if err != nil {
			conn.log.Errorf("set read deadline failed: %v", err)
			return err
		}

		var data []byte
		if echoPing {
			data = []byte(appData)
		}

		// Respond with a pong.
		err = ws.WriteControl(websocket.PongMessage, data, now.Add(writeWait))
		if err != nil {
			// read loop handles reconnect
			conn.log.Errorf("pong write error: %v", err)
			return err
		}

		return nil
	})

	conn.wsMtx.Lock()
	// If keepAlive called connect, the wsConn's current websocket.Conn may need
	// to be closed depending on the error that triggered the reconnect.
	if conn.ws != nil {
		conn.close()
	}
	conn.ws = ws
	conn.wsMtx.Unlock()

	// Before status change so connect handlers see the new endpoint.
	conn.connectedURL.Store(endpoint.url)
	conn.setConnectionStatus(Connected)
	conn.wg.Add(1)
	go func() {
		defer conn.wg.Done()
		if conn.cfg.RawHandler != nil {
			conn.readRaw(ctx, ws)
		} else {
			conn.read(ctx, ws)
		}
	}()

	return nil
}

func (conn *wsConn) SetReadLimit(limit int64) {
	conn.wsMtx.Lock()
	ws := conn.ws
	conn.wsMtx.Unlock()
	if ws != nil {
		ws.SetReadLimit(limit)
	}
}

// handleReadError expires pending requests before queuing a reconnect.
// The reader must return immediately after this call.
func (conn *wsConn) handleReadError(err error) {
	if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) &&
		!errors.Is(err, websocket.ErrCloseSent) {
		conn.log.Errorf("read error (%v), attempting reconnection", err)
	}

	conn.setConnectionStatus(Disconnected)
	// Abort pending requests now instead of waiting for their response timers.
	conn.abortRequests()
	if !conn.cfg.DisableAutoReconnect {
		conn.scheduleReconnect()
	}
}

func (conn *wsConn) close() {
	// Attempt to send a close message in case the connection is still live.
	msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye")
	_ = conn.ws.WriteControl(websocket.CloseMessage, msg,
		time.Now().Add(50*time.Millisecond)) // ignore any error
	// Forcibly close the underlying connection.
	conn.ws.Close()
}

func (conn *wsConn) readRaw(ctx context.Context, ws *websocket.Conn) {
	for {
		// Block until a message is received or an error occurs.
		_, msgBytes, err := ws.ReadMessage()
		// Drop the read error on context cancellation.
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			conn.handleReadError(err)
			return
		}
		conn.cfg.RawHandler(msgBytes)

		err = ws.SetReadDeadline(time.Now().Add(conn.cfg.PingWait))
		if err != nil {
			conn.log.Errorf("set read deadline failed: %v", err)
		}
	}
}

// read fetches and parses incoming messages for processing. This should be
// run as a goroutine. Increment the wg before calling read.
func (conn *wsConn) read(ctx context.Context, ws *websocket.Conn) {
	// overflow buffers messages when readCh is full, preventing the read loop
	// from blocking. This is critical because pings are handled during ReadJSON
	// calls - if the read loop blocks on a channel send, pings won't be
	// processed and the server will disconnect due to ping timeout.
	var overflow []*msgjson.Message

	// drainOverflow attempts to send buffered messages to readCh without blocking.
	drainOverflow := func() {
		for len(overflow) > 0 {
			if ctx.Err() != nil {
				return
			}
			select {
			case conn.readCh <- overflow[0]:
				overflow = overflow[1:]
			default:
				return
			}
		}
	}

	for {
		// Try to drain any overflow before reading new messages.
		if ctx.Err() != nil {
			return
		}
		drainOverflow()

		msg := new(msgjson.Message)

		// The read itself does not require locking since only this goroutine
		// uses read functions that are not safe for concurrent use.
		err := ws.ReadJSON(msg)
		// Drop the read error on context cancellation.
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			var mErr *json.UnmarshalTypeError
			if errors.As(err, &mErr) {
				// JSON decode errors are not fatal, log and proceed.
				conn.log.Errorf("json decode error: %v", mErr)
				continue
			}
			conn.handleReadError(err)
			return
		}

		// If the message is a response, find the handler.
		if msg.Type == msgjson.Response {
			handler := conn.respHandler(msg.ID)
			if handler == nil {
				// The request may have expired or its response may already have
				// been handled.
				b, _ := json.Marshal(msg)
				conn.log.Warnf("No handler found for response: %v", string(b))
				continue
			}
			// Run handlers in a goroutine so that other messages can be
			// received. Include the handler goroutines in the WaitGroup to
			// allow them to complete if the connection master desires.
			conn.wg.Add(1)
			go func() {
				defer conn.wg.Done()
				handler.f(msg)
			}()
			continue
		}

		// Non-blocking send to readCh. If the channel is full, buffer the
		// message to avoid blocking the read loop.
		select {
		case conn.readCh <- msg:
		default:
			// Channel full - buffer the message and warn about backpressure.
			overflow = append(overflow, msg)
			conn.log.Warnf("Read channel full, message buffered (overflow size: %d). "+
				"Consumer may be too slow.", len(overflow))
		}

		err = ws.SetReadDeadline(time.Now().Add(conn.cfg.PingWait))
		if err != nil {
			conn.log.Errorf("set read deadline failed: %v", err)
		}
	}
}

// keepAlive maintains an active websocket connection by reconnecting when
// the established connection is broken. This should be run as a goroutine.
func (conn *wsConn) keepAlive(ctx context.Context) {
	rcInt := reconnectInterval
	for {
		select {
		case <-conn.reconnectCh:
			// Prioritize context cancellation even if there are reconnect
			// requests.
			if ctx.Err() != nil {
				return
			}

			err := conn.connect(ctx)
			if err != nil {
				conn.log.Errorf("Reconnect failed: %v. Retrying in %.1f seconds.", err, rcInt.Seconds())
				time.AfterFunc(rcInt, conn.scheduleReconnect)
				// Increment the wait up to PingWait.
				if rcInt < maxReconnectInterval {
					rcInt += reconnectInterval
				}
				continue
			}

			conn.log.Info("Successfully reconnected.")
			rcInt = reconnectInterval

			// Synchronize after a reconnection.
			if conn.cfg.ReconnectSync != nil {
				conn.cfg.ReconnectSync()
			}

		case <-ctx.Done():
			return
		}
	}
}

// scheduleReconnect queues a reconnect unless one is already pending.
func (conn *wsConn) scheduleReconnect() {
	select {
	case conn.reconnectCh <- struct{}{}:
	default:
	}
}

// abortRequests removes pending requests and runs their expiration callbacks.
func (conn *wsConn) abortRequests() {
	conn.reqMtx.Lock()
	callbacks := make([]func(), 0, len(conn.respHandlers))
	for id, h := range conn.respHandlers {
		delete(conn.respHandlers, id)
		h.expiration.Stop()
		callbacks = append(callbacks, h.abort)
	}
	conn.reqMtx.Unlock()

	// Callbacks may register another request.
	for _, abort := range callbacks {
		abort()
	}
}

// NextID returns the next request id.
func (conn *wsConn) NextID() uint64 {
	return atomic.AddUint64(&conn.rID, 1)
}

// Connect connects the client. Any error encountered during the initial
// connection will be returned. An auto-(re)connect goroutine will be started,
// even on error. To terminate it, use Stop() or cancel the context.
func (conn *wsConn) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	var ctxInternal context.Context
	ctxInternal, conn.cancel = context.WithCancel(ctx)

	err := conn.connect(ctxInternal)
	if err != nil {
		// If the certificate is invalid or missing for the only endpoint, do
		// not start the reconnect loop, and return an error with no WaitGroup.
		if conn.cfg.DisableAutoReconnect || (errors.Is(err, ErrInvalidCert) || errors.Is(err, ErrCertRequired)) &&
			conn.endpoints.count() <= 1 {
			conn.cancel()
			conn.wg.Wait() // probably a no-op
			close(conn.readCh)
			return nil, err
		}

		// The read loop would normally trigger keepAlive, but it wasn't started
		// on account of a connect error.
		conn.log.Errorf("Initial connection failed, starting reconnect loop: %v", err)
		time.AfterFunc(5*time.Second, conn.scheduleReconnect)
	}

	if !conn.cfg.DisableAutoReconnect {
		conn.wg.Add(1)
		go func() {
			defer conn.wg.Done()
			conn.keepAlive(ctxInternal)
		}()
	}

	conn.wg.Add(1)
	go func() {
		defer conn.wg.Done()
		<-ctxInternal.Done()
		conn.setConnectionStatus(Disconnected)
		conn.wsMtx.Lock()
		if conn.ws != nil {
			conn.log.Debug("Sending close 1000 (normal) message.")
			conn.close()
			conn.ws = nil
		}
		conn.wsMtx.Unlock()

		// Run the expire funcs so request callers don't hang.
		conn.abortRequests()

		close(conn.readCh) // signal to MessageSource receivers that the wsConn is dead
	}()

	return &conn.wg, nil
}

// Stop can be used to close the connection and all of the goroutines started by
// Connect. Alternatively, the context passed to Connect may be canceled.
func (conn *wsConn) Stop() {
	conn.cancel()
}

// Send pushes outgoing messages over the websocket connection. Sending of the
// message is synchronous, so a nil error guarantees that the message was
// successfully sent. A non-nil error may indicate that the connection is known
// to be down, the message failed to marshall to JSON, or writing to the
// websocket link failed.
func (conn *wsConn) Send(msg *msgjson.Message) error {
	if conn.IsDown() {
		return fmt.Errorf("cannot send on a broken connection")
	}

	// Marshal the Message first so that we don't send junk to the peer even if
	// it fails to marshal completely, which gorilla/websocket.WriteJSON does.
	b, err := json.Marshal(msg)
	if err != nil {
		conn.log.Errorf("Failed to marshal message: %v", err)
		return err
	}
	return conn.SendRaw(b)
}

// SendRaw sends a raw byte string over the websocket connection.
func (conn *wsConn) SendRaw(b []byte) error {
	if conn.IsDown() {
		return fmt.Errorf("cannot send on a broken connection")
	}

	conn.wsMtx.Lock()
	ws := conn.ws
	conn.wsMtx.Unlock()
	if ws == nil {
		return fmt.Errorf("cannot send on a broken connection")
	}

	conn.writeMtx.Lock()
	defer conn.writeMtx.Unlock()

	if conn.IsDown() {
		return fmt.Errorf("cannot send on a broken connection")
	}

	err := ws.SetWriteDeadline(time.Now().Add(writeWait))
	if err != nil {
		conn.log.Errorf("Send: failed to set write deadline: %v", err)
		return err
	}

	err = ws.WriteMessage(websocket.TextMessage, b)
	if err != nil {
		conn.log.Errorf("Send: WriteMessage error: %v", err)
		return err
	}
	return nil
}

// Request sends the Request-type msgjson.Message to the server and does not
// wait for a response, but records a callback function to run when a response
// is received. A response must be received within DefaultResponseTimeout of the
// request, after which the response handler expires and any late response will
// be ignored. To handle expiration or to set the timeout duration, use
// RequestWithTimeout. Sending of the request is synchronous, so a nil error
// guarantees that the request message was successfully sent.
func (conn *wsConn) Request(msg *msgjson.Message, f func(*msgjson.Message)) error {
	return conn.RequestWithTimeout(msg, f, DefaultResponseTimeout, func() {})
}

func (conn *wsConn) RequestRaw(msgID uint64, rawMsg []byte, f func(*msgjson.Message)) error {
	return conn.RequestRawWithTimeout(msgID, rawMsg, f, DefaultResponseTimeout, func() {})
}

// RequestWithTimeout sends the Request-type message and does not wait for a
// response, but records a callback function to run when a response is received.
// If the server responds within expireTime of the request, the response handler
// is called, otherwise the expire function is called. If the response handler
// is called, it is guaranteed that the response Message.ID is equal to the
// request Message.ID. Sending of the request is synchronous, so a nil error
// guarantees that the request message was successfully sent and that either the
// response handler or expire function will be run; a non-nil error guarantees
// that neither function will run.
//
// For example, to wait on a response or timeout:
//
//	errChan := make(chan error, 1)
//
//	err := conn.RequestWithTimeout(reqMsg, func(msg *msgjson.Message) {
//	    errChan <- msg.UnmarshalResult(responseStructPointer)
//	}, timeout, func() {
//	    errChan <- fmt.Errorf("timed out waiting for '%s' response.", route)
//	})
//	if err != nil {
//	    return err // request error
//	}
//	return <-errChan // timeout or response error
func (conn *wsConn) RequestWithTimeout(msg *msgjson.Message, f func(*msgjson.Message), expireTime time.Duration, expire func()) error {
	if msg.Type != msgjson.Request {
		return fmt.Errorf("Message is not a request: %v", msg.Type)
	}
	rawMsg, err := json.Marshal(msg)
	if err != nil {
		conn.log.Errorf("Failed to marshal message: %v", err)
		return err
	}
	err = conn.RequestRawWithTimeout(msg.ID, rawMsg, f, expireTime, expire)
	if err != nil {
		conn.log.Errorf("(*wsConn).Request(route '%s') Send error (%v), unregistering msg ID %d handler",
			msg.Route, err, msg.ID)
	}
	return err
}

func (conn *wsConn) RequestRawWithTimeout(msgID uint64, rawMsg []byte, f func(*msgjson.Message), expireTime time.Duration, expire func()) error {
	// Register the response and expire handlers for this request.
	conn.logReq(msgID, f, expireTime, expire)
	err := conn.SendRaw(rawMsg)
	if err != nil {
		// Neither expire nor the handler should run. Stop the expire timer
		// created by logReq and delete the response handler it added. The
		// caller receives a non-nil error to deal with it.
		conn.respHandler(msgID) // drop the responseHandler logged by logReq that is no longer necessary
	}
	return err
}

func (conn *wsConn) expire(id uint64) bool {
	conn.reqMtx.Lock()
	defer conn.reqMtx.Unlock()
	_, removed := conn.respHandlers[id]
	delete(conn.respHandlers, id)
	return removed
}

// logReq stores the response handler in the respHandlers map. Requests to the
// client are associated with a response handler.
func (conn *wsConn) logReq(id uint64, respHandler func(*msgjson.Message), expireTime time.Duration, expire func()) {
	conn.reqMtx.Lock()
	defer conn.reqMtx.Unlock()
	doExpire := func() {
		// Delete the response handler, and call the provided expire function if
		// (*wsLink).respHandler has not already retrieved the handler function
		// for execution.
		if conn.expire(id) {
			expire()
		}
	}
	conn.respHandlers[id] = &responseHandler{
		expiration: time.AfterFunc(expireTime, doExpire),
		f:          respHandler,
		abort:      expire,
	}
}

// respHandler extracts the response handler for the provided request ID if it
// exists, else nil. If the handler exists, it will be deleted from the map.
func (conn *wsConn) respHandler(id uint64) *responseHandler {
	conn.reqMtx.Lock()
	defer conn.reqMtx.Unlock()
	cb, ok := conn.respHandlers[id]
	if ok {
		cb.expiration.Stop()
		delete(conn.respHandlers, id)
	}
	return cb
}

// MessageSource returns the connection's read source. The returned chan will
// receive requests and notifications from the server, but not responses, which
// have handlers associated with their request. The same channel is returned on
// each call, so there must only be one receiver. When the connection is
// shutdown, the channel will be closed.
func (conn *wsConn) MessageSource() <-chan *msgjson.Message {
	return conn.readCh
}

type wsEndpoint struct {
	url            string
	cert           []byte
	tlsCfg         *tls.Config
	netDialContext func(context.Context, string, string) (net.Conn, error)
}

// endpointSet holds a nonempty list of prepared endpoints. selected is the
// position retained when replacing the list, not necessarily the live endpoint.
// It starts at the first endpoint and changes on each connection attempt.
type endpointSet struct {
	mtx       sync.RWMutex
	endpoints []*wsEndpoint
	selected  int
	next      int
}

func newWsEndpoint(cfg *WsEndpoint) (*wsEndpoint, error) {
	uri, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL: %w", err)
	}
	switch strings.ToLower(uri.Scheme) {
	case "ws", "wss":
	default:
		return nil, fmt.Errorf("unsupported websocket scheme %q", uri.Scheme)
	}
	if uri.Host == "" {
		return nil, fmt.Errorf("websocket URL host is empty")
	}

	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	cert := append([]byte(nil), cfg.Cert...)
	if len(cert) > 0 {
		if ok := rootCAs.AppendCertsFromPEM(cert); !ok {
			return nil, ErrInvalidCert
		}
	}

	return &wsEndpoint{
		url:            cfg.URL,
		cert:           cert,
		netDialContext: cfg.NetDialContext,
		tlsCfg: &tls.Config{
			RootCAs:    rootCAs,
			MinVersion: tls.VersionTLS12,
			ServerName: uri.Hostname(),
		},
	}, nil
}

func prepareEndpoints(cfgs []*WsEndpoint) ([]*wsEndpoint, error) {
	if len(cfgs) == 0 {
		return nil, fmt.Errorf("empty websocket endpoint list")
	}

	endpoints := make([]*wsEndpoint, 0, len(cfgs))
	seen := make(map[string]struct{}, len(cfgs))
	for _, cfg := range cfgs {
		if cfg == nil {
			return nil, fmt.Errorf("nil websocket endpoint")
		}
		endpoint, err := newWsEndpoint(cfg)
		if err != nil {
			return nil, fmt.Errorf("invalid websocket endpoint %q: %w", cfg.URL, err)
		}
		if _, found := seen[endpoint.url]; found {
			return nil, fmt.Errorf("duplicate websocket endpoint URL %q", endpoint.url)
		}
		seen[endpoint.url] = struct{}{}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}

func (conn *wsConn) certConnectError(endpoint *wsEndpoint, err error) error {
	var connErr error
	if len(endpoint.cert) == 0 {
		connErr = dex.NewError(ErrCertRequired, err.Error())
	} else {
		connErr = dex.NewError(ErrInvalidCert, err.Error())
	}
	if conn.endpoints.count() == 1 {
		conn.setConnectionStatus(InvalidCert)
	} else {
		conn.log.Errorf("Certificate error connecting to websocket endpoint %s: %v", endpoint.url, err)
		conn.setConnectionStatus(Disconnected)
	}
	return connErr
}

func (set *endpointSet) count() int {
	set.mtx.RLock()
	defer set.mtx.RUnlock()
	return len(set.endpoints)
}

func (set *endpointSet) nextEndpoint() *wsEndpoint {
	set.mtx.Lock()
	defer set.mtx.Unlock()
	idx := set.next % len(set.endpoints)
	set.selected = idx
	set.next = (idx + 1) % len(set.endpoints)
	return set.endpoints[idx]
}

func (set *endpointSet) selectedURL() string {
	set.mtx.RLock()
	defer set.mtx.RUnlock()
	return set.endpoints[set.selected].url
}

func (set *endpointSet) selectedUpdateCfg(uri string) *WsEndpoint {
	set.mtx.RLock()
	defer set.mtx.RUnlock()
	selected := set.endpoints[set.selected]
	return &WsEndpoint{
		URL:            uri,
		Cert:           selected.cert,
		NetDialContext: selected.netDialContext,
	}
}

func (set *endpointSet) replaceWithSingle(endpoint *wsEndpoint) {
	set.mtx.Lock()
	set.endpoints = []*wsEndpoint{endpoint}
	set.selected = 0
	set.next = 0
	set.mtx.Unlock()
}

func (set *endpointSet) replace(cfgs []*WsEndpoint) error {
	endpoints, err := prepareEndpoints(cfgs)
	if err != nil {
		return err
	}

	set.mtx.Lock()
	defer set.mtx.Unlock()
	previousURL := set.endpoints[set.selected].url
	set.endpoints = endpoints
	set.selected = 0
	set.next = 0
	for i, endpoint := range endpoints {
		if endpoint.url == previousURL {
			set.selected = i
			set.next = (i + 1) % len(endpoints)
			break
		}
	}
	return nil
}
