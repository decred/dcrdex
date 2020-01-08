package comms

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
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
)

// When the DEX sends a request to the client, a responseHandler is created
// to wait for the response.
type responseHandler struct {
	expiration time.Time
	f          func(*msgjson.Message)
}

// WsCfg is the configuration struct for initializing a WsConn.
type WsCfg struct {
	// URL is the websocket endpoint URL.
	URL string
	// The maximum time in seconds to wait for a ping from the server.
	PingWait time.Duration
	// The rpc certificate file path.
	RpcCert string
	// ReconnectSync runs the needed reconnection synchronisation after
	// a disconnect.
	ReconnectSync func()
	// The dex client context.
	Ctx context.Context
}

// WsConn represents a client websocket connection.
type WsConn struct {
	reconnects   uint64
	rID          uint64
	cfg          *WsCfg
	ws           *websocket.Conn
	wsMtx        sync.Mutex
	tlsCfg       *tls.Config
	readCh       chan *msgjson.Message
	sendCh       chan *msgjson.Message
	reconnectCh  chan struct{}
	reqMtx       sync.RWMutex
	connected    bool
	connectedMtx sync.RWMutex
	once         sync.Once
	wg           sync.WaitGroup
	respHandlers map[uint64]*responseHandler
}

// filesExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	_, err := os.Stat(name)
	return !os.IsNotExist(err)
}

// NewWsConn creates a client websocket connection.
func NewWsConn(cfg *WsCfg) (*WsConn, error) {
	if cfg.PingWait < 0 {
		return nil, fmt.Errorf("ping wait cannot be negative")
	}

	var tlsConfig *tls.Config
	if cfg.RpcCert != "" {
		if !fileExists(cfg.RpcCert) {
			return nil, fmt.Errorf("the rpc cert provided (%v) "+
				"does not exist", cfg.RpcCert)
		}

		rootCAs, _ := x509.SystemCertPool()
		if rootCAs == nil {
			rootCAs = x509.NewCertPool()
		}

		certs, err := ioutil.ReadFile(cfg.RpcCert)
		if err != nil {
			return nil, fmt.Errorf("file reading error: %v", err)
		}

		if ok := rootCAs.AppendCertsFromPEM(certs); !ok {
			return nil, fmt.Errorf("unable to append cert")
		}

		tlsConfig = &tls.Config{
			RootCAs:    rootCAs,
			MinVersion: tls.VersionTLS12,
		}
	}

	conn := &WsConn{
		cfg:          cfg,
		tlsCfg:       tlsConfig,
		readCh:       make(chan *msgjson.Message, readBuffSize),
		sendCh:       make(chan *msgjson.Message),
		reconnectCh:  make(chan struct{}),
		respHandlers: make(map[uint64]*responseHandler),
	}

	conn.wg.Add(1)
	go conn.keepAlive()
	conn.reconnectCh <- struct{}{}

	return conn, nil
}

// isConnected returns the connection connected state.
func (conn *WsConn) isConnected() bool {
	conn.connectedMtx.RLock()
	defer conn.connectedMtx.RUnlock()
	return conn.connected
}

// setConnected updates the connection's connected state.
func (conn *WsConn) setConnected(connected bool) {
	conn.connectedMtx.Lock()
	conn.connected = connected
	conn.connectedMtx.Unlock()
}

// NextID returns the next request id.
func (conn *WsConn) NextID() uint64 {
	return atomic.AddUint64(&conn.rID, 1)
}

// close terminates all websocket processes and closes the connection.
func (conn *WsConn) close() {
	conn.wsMtx.Lock()
	defer conn.wsMtx.Unlock()

	if conn.ws == nil {
		return
	}

	msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
	conn.ws.WriteControl(websocket.CloseMessage, msg,
		time.Now().Add(writeWait))
	conn.ws.Close()
}

// connect attempts to establish a websocket connection.
func (conn *WsConn) connect() error {
	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig:  conn.tlsCfg,
	}

	ws, _, err := dialer.Dial(conn.cfg.URL, nil)
	if err != nil {
		return err
	}

	ws.SetPingHandler(func(string) error {
		conn.wsMtx.Lock()
		defer conn.wsMtx.Unlock()

		now := time.Now()
		err := ws.SetReadDeadline(now.Add(conn.cfg.PingWait))
		if err != nil {
			log.Errorf("read deadline error: %v", err)
			return err
		}

		// Respond with a pong.
		err = ws.WriteControl(websocket.PongMessage, []byte{}, now.Add(writeWait))
		if err != nil {
			log.Errorf("pong error: %v", err)
			return err
		}

		return nil
	})

	conn.wsMtx.Lock()
	conn.ws = ws
	conn.wsMtx.Unlock()

	return nil
}

// read fetches and parses incoming messages for processing. This should be
// run as a goroutine.
func (conn *WsConn) read() {
	defer conn.wg.Done()

	for {
		msg := new(msgjson.Message)

		conn.wsMtx.Lock()
		ws := conn.ws
		conn.wsMtx.Unlock()

		err := ws.ReadJSON(msg)
		if err != nil {
			var mErr *json.UnmarshalTypeError
			if errors.As(err, &mErr) {
				// JSON decode errors are not fatal, log and proceed.
				log.Errorf("json decode error: %v", mErr)
				continue
			}

			if websocket.IsCloseError(err, websocket.CloseGoingAway,
				websocket.CloseNormalClosure) ||
				strings.Contains(err.Error(), "websocket: close sent") {
				return
			}

			var opErr *net.OpError
			if errors.As(err, &opErr) {
				if opErr.Op == "read" {
					if strings.Contains(opErr.Err.Error(),
						"use of closed network connection") {
						return
					}
				}
			}

			// Log all other errors and trigger a reconnection.
			log.Errorf("read error: %v", err)
			conn.reconnectCh <- struct{}{}
			return
		}

		// If the message is a response, find the handler.
		if msg.Type == msgjson.Response {
			handler := conn.respHandler(msg.ID)
			if handler == nil {
				b, _ := json.Marshal(msg)
				log.Errorf("no handler found for response", string(b))
			}
			handler.f(msg)
			continue
		}

		conn.readCh <- msg
	}
}

// keepAlive maintains an active websocket connection by reconnecting when
// the established connection is broken. This should be run as a goroutine.
func (conn *WsConn) keepAlive() {
	for {
		select {
		case <-conn.reconnectCh:
			conn.setConnected(false)

			reconnects := atomic.AddUint64(&conn.reconnects, 1)
			if reconnects > 1 {
				conn.close()
			}

			err := conn.connect()
			if err != nil {
				log.Errorf("connection error: %v", err)

				go func() {
					// Attempt to reconnect.
					time.Sleep(conn.cfg.PingWait)
					conn.reconnectCh <- struct{}{}
				}()

				continue
			}

			conn.wg.Add(1)
			go conn.read()

			// Synchronize after a reconnection.
			if conn.cfg.ReconnectSync != nil {
				conn.cfg.ReconnectSync()
			}

			conn.setConnected(true)

		case <-conn.cfg.Ctx.Done():
			// Terminate the keepAlive process and read process when
			/// the dex client signals a shutdown.
			conn.setConnected(false)
			conn.close()
			conn.wg.Done()
			return
		}
	}
}

// WaitForShutdown blocks until the websocket's processes are stopped.
func (conn *WsConn) WaitForShutdown() {
	conn.wg.Wait()
}

// Send pushes outgoing messages over the websocket connection.
func (conn *WsConn) Send(msg *msgjson.Message) error {
	if !conn.isConnected() {
		return fmt.Errorf("cannot send on a broken connection")
	}

	conn.wsMtx.Lock()
	conn.ws.SetWriteDeadline(time.Now().Add(writeWait))
	err := conn.ws.WriteJSON(msg)
	conn.wsMtx.Unlock()
	if err != nil {
		log.Errorf("write error: %v", err)
		return err
	}
	return nil
}

// Request sends the message with Send, but keeps a record of the callback
// function to run when a response is recieved.
func (conn *WsConn) Request(msg *msgjson.Message, f func(*msgjson.Message)) error {
	// Log the message sent if it is a request.
	if msg.Type == msgjson.Request {
		conn.logReq(msg.ID, f)
	}
	return conn.Send(msg)
}

// logReq stores the response handler in the respHandlers map. Requests to the
// client are associated with a response handler.
func (conn *WsConn) logReq(id uint64, respHandler func(*msgjson.Message)) {
	conn.reqMtx.Lock()
	defer conn.reqMtx.Unlock()
	conn.respHandlers[id] = &responseHandler{
		expiration: time.Now().Add(time.Minute * 5),
		f:          respHandler,
	}
	// clean up the response map.
	if len(conn.respHandlers) > 1 {
		go conn.cleanUpExpired()
	}
}

// cleanUpExpired cleans up the response handler map.
func (conn *WsConn) cleanUpExpired() {
	conn.reqMtx.Lock()
	defer conn.reqMtx.Unlock()
	var expired []uint64
	for id, cb := range conn.respHandlers {
		if time.Until(cb.expiration) < 0 {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		delete(conn.respHandlers, id)
	}
}

// respHandler extracts the response handler for the provided request ID if it
// exists, else nil. If the handler exists, it will be deleted from the map.
func (conn *WsConn) respHandler(id uint64) *responseHandler {
	conn.reqMtx.Lock()
	defer conn.reqMtx.Unlock()
	cb, ok := conn.respHandlers[id]
	if ok {
		delete(conn.respHandlers, id)
	}
	return cb
}

// MessageSource returns the connection's read source only once. The returned
// chan will receive requests and notifications from the server, but not
// responses, which have handlers associated with their request.
func (conn *WsConn) MessageSource() <-chan *msgjson.Message {
	var ch <-chan *msgjson.Message

	conn.once.Do(func() {
		ch = conn.readCh
	})

	return ch
}
