package comms

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrdex/server/comms/msgjson"
	"github.com/gorilla/websocket"
)

const (
	// pingWait is the maximum time in seconds to wait for a ping from
	// the server. The client can be reconnected after this threshold since
	// the websocket connection to the server is assumed to broken.
	pingWait = 60 * time.Second

	// writeWait is the maximum time in seconds to write to the connection.
	writeWait = 5 * time.Second

	// bufferSize is buffer size for the websocket connection's read channel.
	readBuffSize = 128
)

// WsCfg is the configuration struct for initializing a WsConn.
type WsCfg struct {
	// The websocket host.
	Host string
	// The websocket api path.
	Path string
	// The maximum time in seconds to wait for a ping from the server.
	PingWait time.Duration
	// The maximum time in seconds to write to the connection.
	WriteWait time.Duration
	// The rpc certificate file path.
	RpcCert string
	// The rpc key file path.
	RpcKey string
	// Skip server cert verification, should only be used in testing.
	InsecureSkipVerify bool
	// ReconnectSync runs the needed reconnection synchronisation after
	// a disconnect.
	ReconnectSync func()
	// The context.
	Ctx context.Context
	// The context cancel func.
	Cancel context.CancelFunc
}

// WsConn represents a client websocket connection.
type WsConn struct {
	rID          uint64
	cfg          *WsCfg
	ws           *websocket.Conn
	tlsCfg       *tls.Config
	readCh       chan *msgjson.Message
	sendCh       chan *msgjson.Message
	req          map[uint64]*msgjson.Message
	reqMtx       sync.RWMutex
	writeMtx     sync.Mutex
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
	if cfg.PingWait < time.Second {
		return nil, fmt.Errorf("ping wait time should be a second or greater")
	}

	if cfg.WriteWait < time.Second {
		return nil, fmt.Errorf("write wait time should be a second or greater")
	}

	var tlsConfig *tls.Config
	if fileExists(cfg.RpcCert) && fileExists(cfg.RpcKey) {
		keypair, err := tls.LoadX509KeyPair(cfg.RpcCert, cfg.RpcKey)
		if err != nil {
			return nil, err
		}

		// Prepare the TLS configuration.
		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{keypair},
			MinVersion:   tls.VersionTLS12,
		}

		if cfg.InsecureSkipVerify {
			tlsConfig.InsecureSkipVerify = true
		}
	} else {
		if cfg.RpcCert != "" {
			log.Warn("the provided rpc cert does not exist")
		}

		if cfg.RpcKey != "" {
			log.Warn("the provided rpc key does not exist")
		}
	}

	conn := &WsConn{
		cfg:    cfg,
		tlsCfg: tlsConfig,
		readCh: make(chan *msgjson.Message, readBuffSize),
		sendCh: make(chan *msgjson.Message),
		req:    make(map[uint64]*msgjson.Message),
	}

	err := conn.connect()
	if err != nil {
		return nil, fmt.Errorf("unable to create websocket: %v", err)
	}

	return conn, nil
}

// isConnected returns the connection connected state.
func (conn *WsConn) isConnected() bool {
	conn.connectedMtx.RLock()
	defer conn.connectedMtx.RUnlock()
	return conn.connected
}

// setConnected updates the connection's connected state
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

// connect attempts to establish a websocket connection.
func (conn *WsConn) connect() error {
	scheme := "ws"

	if conn.tlsCfg != nil {
		scheme = "wss"
	}

	url := url.URL{
		Scheme: scheme,
		Host:   conn.cfg.Host,
		Path:   conn.cfg.Path,
	}

	dialer := &websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
	}

	if conn.tlsCfg != nil {
		dialer.TLSClientConfig = conn.tlsCfg
	}

	var err error
	conn.ws, _, err = dialer.Dial(url.String(), nil)
	if err != nil {
		return err
	}

	conn.setConnected(true)
	conn.ws.SetPingHandler(func(string) error {
		now := time.Now()
		err := conn.ws.SetReadDeadline(now.Add(conn.cfg.PingWait))
		if err != nil {
			log.Errorf("unable to set read deadline: %v", err)
			return err
		}

		// Respond with a pong.
		return conn.ws.WriteControl(websocket.PongMessage, []byte{},
			now.Add(conn.cfg.WriteWait))
	})

	return nil
}

// keepAlive maintains an active websocket connection by reconnecting when
// the established connection is broken. This should be run as a goroutine.
func (conn *WsConn) keepAlive() {
	ticker := time.NewTicker(time.Second)

	for {
		select {
		case <-ticker.C:
			if !conn.isConnected() {
				err := conn.connect()
				if err != nil {
					log.Errorf("unable to establish a connection: %v", err)
				}

				// Run the reconnect sync after reestablishing a connection.
				if conn.cfg.ReconnectSync != nil {
					conn.cfg.ReconnectSync()
				}
			}

		case <-conn.cfg.Ctx.Done():
			now := time.Now()
			closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")
			conn.writeMtx.Lock()
			err := conn.ws.WriteControl(websocket.CloseMessage, closeMsg,
				now.Add(conn.cfg.WriteWait))
			conn.writeMtx.Unlock()
			if err != nil {
				log.Errorf("unable to close websocket connection: %v", err)
			}
			ticker.Stop()
			conn.wg.Done()
			return
		}
	}
}

// read fetches and parses incoming messages for processing. This should be
// run as a goroutine.
func (conn *WsConn) read() {
	msg := new(msgjson.Message)

	for {
		err := conn.ws.ReadJSON(msg)
		if err != nil {
			if opErr, ok := err.(*net.OpError); ok {
				if opErr.Op == "read" {
					conn.setConnected(false)

					// Log the read error and proceed for a reconnection.
					log.Debugf("read op error: %v", opErr.Err)
					time.Sleep(time.Millisecond * 500)
					continue
				}
			}

			if websocket.IsCloseError(err, websocket.CloseGoingAway,
				websocket.CloseNormalClosure) ||
				strings.Contains(err.Error(), "websocket: close sent") {
				// Return on a normal close error message.
				conn.cfg.Cancel()
				return
			}

			if nErr, ok := err.(net.Error); ok {
				if nErr.Timeout() {
					conn.setConnected(false)

					// Proceed for a reconnection on a timeout error.
					log.Debugf("read timeout: %v", nErr)
					time.Sleep(time.Millisecond * 500)
					continue
				}
			}

			if _, ok := err.(*json.UnmarshalTypeError); ok {
				// Log the JSON error and proceed.
				log.Errorf("unable to decode JSON: %v", err)
				continue
			}

			log.Errorf("unable to read JSON: %v", err)
			conn.cfg.Cancel()
			return
		}

		conn.readCh <- msg
	}
}

// send pushes outgoing messages over the websocket connection.
// This should be run as a goroutine.
func (conn *WsConn) send() {
	for {
		select {
		case msg := <-conn.sendCh:
			// Log the route if the message being sent is a request.
			if msg.Type == msgjson.Request {
				conn.logRequest(msg.ID, msg)
			}

			conn.writeMtx.Lock()
			_ = conn.ws.SetWriteDeadline(time.Now().Add(conn.cfg.WriteWait))
			err := conn.ws.WriteJSON(msg)
			conn.writeMtx.Unlock()
			if err != nil {
				conn.setConnected(false)

				// Log the write error and proceed for a reconnection.
				log.Errorf("unable to write message: %v", err)
				continue
			}

		case <-conn.cfg.Ctx.Done():
			conn.wg.Done()
			return
		}
	}
}

// Run starts all processes of the websocket connection.
func (conn *WsConn) Run() {
	go conn.read()

	conn.wg.Add(2)
	go conn.keepAlive()
	go conn.send()
}

// WaitForShutdown waits for all monitored connection processes to terminate.
func (conn *WsConn) WaitForShutdown() {
	conn.wg.Wait()
}

// SendMessage pushes an outgoing message to send channel.
func (conn *WsConn) SendMessage(msg *msgjson.Message) error {
	if !conn.isConnected() {
		return fmt.Errorf("cannot send on a broken connection")
	}

	go func() {
		conn.sendCh <- msg
	}()

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
