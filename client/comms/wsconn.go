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
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"github.com/gorilla/websocket"
)

const (
	// bufferSize is buffer size for a websocket connection's read channel.
	readBuffSize = 128

	// The maximum time in seconds to write to a connection.
	writeWait = time.Second * 3

	// reconnetInterval is the initial and increment between reconnect tries.
	reconnectInterval = 5 * time.Second

	// maxReconnetInterval is the maximum allowed reconnect interval.
	maxReconnectInterval = time.Minute
)

// ErrInvalidCert is the error returned when attempting to use an invalid cert
// to set up a ws connection.
var ErrInvalidCert = fmt.Errorf("invalid certificate")

// ErrCertRequired is the error returned when a ws connection fails because no
// cert was provided.
var ErrCertRequired = fmt.Errorf("certificate required")

// WsConn is an interface for a websocket client.
type WsConn interface {
	NextID() uint64
	Send(msg *msgjson.Message) error
	Request(msg *msgjson.Message, f func(*msgjson.Message)) error
	Connect(ctx context.Context) (*sync.WaitGroup, error)
	MessageSource() <-chan *msgjson.Message
}

// When the DEX sends a request to the client, a responseHandler is created
// to wait for the response.
type responseHandler struct {
	expiration *time.Timer
	f          func(*msgjson.Message)
}

// WsCfg is the configuration struct for initializing a WsConn.
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
	ConnectEventFunc func(bool)
}

// wsConn represents a client websocket connection.
type wsConn struct {
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	reconnects   uint64
	rID          uint64
	cfg          *WsCfg
	ws           *websocket.Conn
	wsMtx        sync.Mutex
	tlsCfg       *tls.Config
	readCh       chan *msgjson.Message
	reconnectCh  chan struct{}
	reqMtx       sync.RWMutex
	connected    bool
	connectedMtx sync.RWMutex
	respHandlers map[uint64]*responseHandler
}

// filesExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	_, err := os.Stat(name)
	return !os.IsNotExist(err)
}

// NewWsConn creates a client websocket connection.
func NewWsConn(cfg *WsCfg) (WsConn, error) {
	if cfg.PingWait < 0 {
		return nil, fmt.Errorf("ping wait cannot be negative")
	}

	var tlsConfig *tls.Config
	if len(cfg.Cert) > 0 {

		uri, err := url.Parse(cfg.URL)
		if err != nil {
			return nil, fmt.Errorf("error parsing URL: %v", err)
		}

		rootCAs, _ := x509.SystemCertPool()
		if rootCAs == nil {
			rootCAs = x509.NewCertPool()
		}

		if ok := rootCAs.AppendCertsFromPEM(cfg.Cert); !ok {
			return nil, ErrInvalidCert
		}

		tlsConfig = &tls.Config{
			RootCAs:    rootCAs,
			MinVersion: tls.VersionTLS12,
			ServerName: uri.Hostname(),
		}
	}

	return &wsConn{
		cfg:          cfg,
		tlsCfg:       tlsConfig,
		readCh:       make(chan *msgjson.Message, readBuffSize),
		reconnectCh:  make(chan struct{}, 1),
		respHandlers: make(map[uint64]*responseHandler),
	}, nil
}

// isConnected returns the connection connected state.
func (conn *wsConn) isConnected() bool {
	conn.connectedMtx.RLock()
	defer conn.connectedMtx.RUnlock()
	return conn.connected
}

// setConnected updates the connection's connected state and runs the
// ConnectEventFunc in case of a change.
func (conn *wsConn) setConnected(connected bool) {
	conn.connectedMtx.Lock()
	statusChange := conn.connected != connected
	conn.connected = connected
	conn.connectedMtx.Unlock()
	if statusChange && conn.cfg.ConnectEventFunc != nil {
		conn.cfg.ConnectEventFunc(connected)
	}
}

// connect attempts to establish a websocket connection.
func (conn *wsConn) connect(ctx context.Context) error {
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig:  conn.tlsCfg,
	}

	ws, _, err := dialer.Dial(conn.cfg.URL, nil)
	if err != nil {
		if _, isUnknownAuthError := err.(x509.UnknownAuthorityError); isUnknownAuthError {
			if conn.tlsCfg == nil {
				return ErrCertRequired
			}
			return ErrInvalidCert
		}
		return err
	}

	// Set the initial read deadline for the first ping. Subsequent read
	// deadlines are set in the ping handler.
	err = ws.SetReadDeadline(time.Now().Add(conn.cfg.PingWait))
	if err != nil {
		log.Errorf("set read deadline failed: %v", err)
		return err
	}

	ws.SetPingHandler(func(string) error {
		now := time.Now()

		// Set the deadline for the next ping.
		err := ws.SetReadDeadline(now.Add(conn.cfg.PingWait))
		if err != nil {
			log.Errorf("set read deadline failed: %v", err)
			return err
		}

		// Respond with a pong.
		err = ws.WriteControl(websocket.PongMessage, []byte{}, now.Add(writeWait))
		if err != nil {
			log.Errorf("pong error: %v", err)
			return err
		}
		log.Tracef("got pinged, and ponged the server")

		return nil
	})

	conn.wsMtx.Lock()
	// If keepAlive called connect, the wsConn's current websocket.Conn may need
	// to be closed depending on the error that triggered the reconnect.
	if conn.ws != nil {
		// Attempt to send a close message in case the connection is still live.
		msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye")
		conn.ws.WriteControl(websocket.CloseMessage, msg,
			time.Now().Add(50*time.Millisecond)) // ignore any error
		// Forcibly close the underlying connection.
		conn.ws.Close()
	}
	conn.ws = ws
	conn.wsMtx.Unlock()

	conn.setConnected(true)
	conn.wg.Add(1)
	go func() {
		defer conn.wg.Done()
		conn.read(ctx)
	}()

	return nil
}

// read fetches and parses incoming messages for processing. This should be
// run as a goroutine. Increment the wg before calling read.
func (conn *wsConn) read(ctx context.Context) {
	for {
		msg := new(msgjson.Message)

		// Lock since conn.ws may be set by connect.
		conn.wsMtx.Lock()
		ws := conn.ws
		conn.wsMtx.Unlock()

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
				log.Errorf("json decode error: %v", mErr)
				continue
			}

			// TODO: Now that wsConn goroutines have contexts that are canceled
			// on shutdown, we do not have to infer the source and severity of
			// the error; just reconnect in ALL other cases, and remove the
			// following legacy checks.

			// Expected close errors (1000 and 1001) ... but if the server
			// closes we still want to reconnect. (???)
			if websocket.IsCloseError(err, websocket.CloseGoingAway,
				websocket.CloseNormalClosure) ||
				strings.Contains(err.Error(), "websocket: close sent") {
				conn.reconnect()
				return
			}

			var opErr *net.OpError
			if errors.As(err, &opErr) && opErr.Op == "read" {
				if strings.Contains(opErr.Err.Error(),
					"use of closed network connection") {
					log.Errorf("read quiting: %v", err)
					conn.reconnect()
					return
				}
			}

			// Log all other errors and trigger a reconnection.
			log.Errorf("read error (%v), attempting reconnection", err)
			conn.reconnect()
			// Successful reconnect via connect() will start read() again.
			return
		}

		// If the message is a response, find the handler.
		if msg.Type == msgjson.Response {
			handler := conn.respHandler(msg.ID)
			if handler == nil {
				b, _ := json.Marshal(msg)
				log.Errorf("No handler found for response: %v", string(b))
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
		conn.readCh <- msg
	}
}

// keepAlive maintains an active websocket connection by reconnecting when
// the established connection is broken. This should be run as a goroutine.
func (conn *wsConn) keepAlive(ctx context.Context) {
	maxReconnects := uint64(20000000) // TODO: reason to limit this?
	rcInt := reconnectInterval
	for {
		select {
		case <-conn.reconnectCh:
			// Prioritize context cancellation even if there are reconnect
			// requests.
			if ctx.Err() != nil {
				return
			}

			if conn.reconnects >= maxReconnects {
				log.Error("Max reconnection attempts reached. Stopping connection.")
				conn.cancel()
				// The WaitGroup provided to the consumer by Connect will start
				// to be decremented as goroutines shutdown.
				return
			}

			log.Infof("Attempting to reconnect to %s...", conn.cfg.URL)
			err := conn.connect(ctx)
			if err != nil {
				log.Errorf("Reconnect failed: %v", err)
				conn.reconnects++
				if conn.reconnects+1 < maxReconnects {
					conn.queueReconnect(rcInt)
					// Increment the wait up to PingWait.
					if rcInt < maxReconnectInterval {
						rcInt += reconnectInterval
					}
				}
				continue
			}

			log.Info("Successfully reconnected.")
			conn.reconnects = 0
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

// reconnect begins reconnection immediately.
func (conn *wsConn) reconnect() {
	conn.setConnected(false)
	conn.reconnectCh <- struct{}{}
}

// queueReconnect queues a reconnection attempt.
func (conn *wsConn) queueReconnect(wait time.Duration) {
	conn.setConnected(false)
	log.Infof("Attempting reconnect to %s in %d seconds.", conn.cfg.URL, wait/time.Second)
	time.AfterFunc(wait, func() { conn.reconnectCh <- struct{}{} })
}

// NextID returns the next request id.
func (conn *wsConn) NextID() uint64 {
	return atomic.AddUint64(&conn.rID, 1)
}

// Connect connects the client. Any error encountered during the initial
// connection will be returned. If the connection is successful, an
// auto-reconnect goroutine will be started. To shutdown auto-reconnect, use
// Stop() or cancel the context.
func (conn *wsConn) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	var ctxInternal context.Context
	ctxInternal, conn.cancel = context.WithCancel(ctx)

	conn.wg.Add(1)
	go func() {
		defer conn.wg.Done()
		conn.keepAlive(ctxInternal)
	}()

	conn.wg.Add(1)
	go func() {
		defer conn.wg.Done()
		<-ctxInternal.Done()
		conn.setConnected(false)
		if conn.ws != nil {
			log.Debug("Sending close 1000 (normal) message.")
			msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "bye")
			conn.ws.WriteControl(websocket.CloseMessage, msg,
				time.Now().Add(writeWait))
			conn.ws.Close()
		}
		close(conn.readCh) // signal to receivers that the wsConn is dead
	}()

	return &conn.wg, conn.connect(ctxInternal)
}

// Stop can be used to close the connection and all of the goroutines started by
// Connect. Alternatively, the context passed to Connect may be canceled.
func (conn *wsConn) Stop() {
	conn.cancel()
}

// Send pushes outgoing messages over the websocket connection.
func (conn *wsConn) Send(msg *msgjson.Message) error {
	if !conn.isConnected() {
		return fmt.Errorf("cannot send on a broken connection")
	}

	conn.wsMtx.Lock()
	defer conn.wsMtx.Unlock()
	err := conn.ws.SetWriteDeadline(time.Now().Add(writeWait))
	if err != nil {
		log.Errorf("failed to set write deadline: %v", err)
		return err
	}

	err = conn.ws.WriteJSON(msg)
	if err != nil {
		log.Errorf("write error: %v", err)
		return err
	}
	return nil
}

// Request sends the message with Send, but keeps a record of the callback
// function to run when a response is received.
func (conn *wsConn) Request(msg *msgjson.Message, f func(*msgjson.Message)) error {
	// Log the message sent if it is a request.
	if msg.Type == msgjson.Request {
		conn.logReq(msg.ID, f)
	}
	return conn.Send(msg)
}

// logReq stores the response handler in the respHandlers map. Requests to the
// client are associated with a response handler.
func (conn *wsConn) logReq(id uint64, respHandler func(*msgjson.Message)) {
	conn.reqMtx.Lock()
	defer conn.reqMtx.Unlock()
	conn.respHandlers[id] = &responseHandler{
		expiration: time.AfterFunc(time.Minute*5, func() {
			conn.reqMtx.Lock()
			delete(conn.respHandlers, id)
			conn.reqMtx.Unlock()
		}),
		f: respHandler,
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
