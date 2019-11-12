package comms

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrdex/dex/msgjson"
	"github.com/gorilla/websocket"
)

const (
	// bufferSize is buffer size for a websocket connection's read channel.
	readBuffSize = 128

	// The maximum time in seconds to write to a connection.
	writeWait = time.Second * 3
)

// WsCfg is the configuration struct for initializing a WsConn.
type WsCfg struct {
	// The websocket host.
	Host string
	// The websocket api path.
	Path string
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
	req          map[uint64]*msgjson.Message
	reqMtx       sync.RWMutex
	connected    bool
	connectedMtx sync.RWMutex
	once         sync.Once
	wg           sync.WaitGroup
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
		cfg:         cfg,
		tlsCfg:      tlsConfig,
		readCh:      make(chan *msgjson.Message, readBuffSize),
		sendCh:      make(chan *msgjson.Message),
		reconnectCh: make(chan struct{}),
		req:         make(map[uint64]*msgjson.Message),
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

// logRoute logs a request keyed by its id.
func (conn *WsConn) logRequest(id uint64, req *msgjson.Message) {
	conn.reqMtx.Lock()
	conn.req[id] = req
	conn.reqMtx.Unlock()
}

// FetchRequest fetches the request associated with the id. The returned
// request is removed from the cache.
func (conn *WsConn) FetchRequest(id uint64) (*msgjson.Message, error) {
	conn.reqMtx.Lock()
	defer conn.reqMtx.Unlock()
	req := conn.req[id]
	if req == nil {
		return nil, fmt.Errorf("no request found for id %d", id)
	}

	delete(conn.req, id)

	return req, nil
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
	url := url.URL{
		Scheme: "wss",
		Host:   conn.cfg.Host,
		Path:   conn.cfg.Path,
	}

	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig:  conn.tlsCfg,
	}

	ws, _, err := dialer.Dial(url.String(), nil)
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
			if _, ok := err.(*json.UnmarshalTypeError); ok {
				// JSON decode errors are not fatal, log and proceed.
				log.Errorf("json decode error: %v", err)
				continue
			}

			if websocket.IsCloseError(err, websocket.CloseGoingAway,
				websocket.CloseNormalClosure) ||
				strings.Contains(err.Error(), "websocket: close sent") {
				return
			}

			if opErr, ok := err.(*net.OpError); ok {
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

	// Log the message sent if it is a request.
	if msg.Type == msgjson.Request {
		conn.logRequest(msg.ID, msg)
	}

	return nil
}

// FetchReadSource returns the connection's read source only once.
func (conn *WsConn) FetchReadSource() <-chan *msgjson.Message {
	var ch <-chan *msgjson.Message

	conn.once.Do(func() {
		ch = conn.readCh
	})

	return ch
}
