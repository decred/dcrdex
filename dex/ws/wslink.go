// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"github.com/gorilla/websocket"
)

// outBufferSize is the size of the WSLink's buffered channel for outgoing
// messages.
const outBufferSize = 128

const writeWait = 5 * time.Second

// websocket.Upgrader is the preferred method of upgrading a request to a
// websocket connection.
var upgrader = websocket.Upgrader{}

// Error is just a basic error.
type Error string

// Error satisfies the error interface.
func (e Error) Error() string {
	return string(e)
}

// ErrPeerDisconnected will be returned if Send or Request is called on a
// disconnected link.
const ErrPeerDisconnected = Error("peer disconnected")

// Connection represents a websocket connection to a remote peer. In practice,
// it is satisfied by *websocket.Conn. For testing, a stub can be used.
type Connection interface {
	Close() error

	SetReadDeadline(t time.Time) error
	ReadMessage() (int, []byte, error)

	SetWriteDeadline(t time.Time) error
	WriteMessage(int, []byte) error
	WriteControl(messageType int, data []byte, deadline time.Time) error
}

// WSLink is the local, per-connection representation of a DEX peer (client or
// server) connection.
type WSLink struct {
	// ip is the peer's IP address.
	ip string
	// conn is the gorilla websocket.Conn, or a stub for testing.
	conn Connection
	// writeMtx is a mutex to sequence WriteMessage operations.
	writeMtx sync.Mutex
	// on is used internally to prevent multiple Close calls on the underlying
	// connections.
	on uint32
	// quit is used to cancel the Context.
	quit context.CancelFunc
	// outChan is used to sequence sent messages.
	outChan chan []byte
	// The WSLink has at least 3 goroutines, one for read, one for write, and
	// one server goroutine to monitor for peer disconnection. The WaitGroup is
	// used to synchronize cleanup on disconnection.
	wg sync.WaitGroup
	// A master message handler.
	handler func(*msgjson.Message) *msgjson.Error
	// pingPeriod is how often to ping the peer.
	pingPeriod time.Duration
	// sendWG is a WaitGroup to sequence sends with a graceful shutdown.
	sendWG sync.WaitGroup
}

// NewWSLink is a constructor for a new WSLink.
func NewWSLink(addr string, conn Connection, pingPeriod time.Duration, handler func(*msgjson.Message) *msgjson.Error) *WSLink {
	return &WSLink{
		ip:         addr,
		conn:       conn,
		outChan:    make(chan []byte, outBufferSize),
		pingPeriod: pingPeriod,
		handler:    handler,
	}
}

// Send sends the passed Message to the websocket peer.
func (c *WSLink) Send(msg *msgjson.Message) error {
	if c.Off() {
		return ErrPeerDisconnected
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	c.sendWG.Add(1)
	c.outChan <- b
	go func() {
		c.writeMtx.Lock()
		b := <-c.outChan
		c.conn.SetWriteDeadline(time.Now().Add(writeWait))
		err := c.conn.WriteMessage(websocket.TextMessage, b)
		c.writeMtx.Unlock()
		c.sendWG.Done()
		if err != nil {
			c.Disconnect()
		}
	}()
	return nil
}

// SendError sends the msgjson.Error to the peer.
func (c *WSLink) SendError(id uint64, rpcErr *msgjson.Error) {
	msg, err := msgjson.NewResponse(id, nil, rpcErr)
	if err != nil {
		log.Errorf("SendError: failed to create message: %v", err)
	}
	err = c.Send(msg)
	if err != nil {
		log.Debug("SendError: failed to send message to peer %s: %v", c.ip, err)
	}
}

// Connect begins processing input and output messages.
func (c *WSLink) Connect(ctx context.Context) (error, *sync.WaitGroup) {
	// Set the initial read deadline now that the ping ticker is about to be
	// started. The pong handler will set subsequent read deadlines. 2x ping
	// period is a very generous initial pong wait; the readWait provided to
	// NewConnection could be stored and used here (once) instead.
	if !atomic.CompareAndSwapUint32(&c.on, 0, 1) {
		return fmt.Errorf("Attempted to Start a running WSLink"), nil
	}
	linkCtx, quit := context.WithCancel(ctx)
	c.quit = quit
	err := c.conn.SetReadDeadline(time.Now().Add(c.pingPeriod * 2))
	if err != nil {
		return fmt.Errorf("Failed to set initial read deadline for %v: %v", c.ip, err), nil
	}

	log.Tracef("Starting websocket messaging with peer %s", c.ip)
	// Start processing input and output.
	c.wg.Add(2)
	go c.inHandler(linkCtx)
	go c.pingHandler(linkCtx)
	return nil, &c.wg
}

// Disconnect closes both the underlying websocket connection and the quit
// channel.
func (c *WSLink) Disconnect() {
	if !atomic.CompareAndSwapUint32(&c.on, 1, 0) {
		log.Debugf("Disconnect attempted on stopped WSLink.")
		return
	}
	log.Tracef("Closing connection with peer %v", c.ip)
	c.quit()
	c.sendWG.Wait()
	c.conn.Close()
}

// inHandler handles all incoming messages for the websocket connection. It must
// be run as a goroutine.
func (c *WSLink) inHandler(ctx context.Context) {
	// Ensure the connection is closed.
	defer c.Disconnect()
	defer c.wg.Done()
out:
	for {
		// Quit when the context is closed.
		if ctx.Err() != nil {
			break out
		}
		// Block until a message is received or an error occurs.
		_, msgBytes, err := c.conn.ReadMessage()
		if err != nil {
			// Log the error if it's not due to disconnecting.
			if !websocket.IsCloseError(err, websocket.CloseGoingAway,
				websocket.CloseNormalClosure, websocket.CloseNoStatusReceived) {
				log.Errorf("Websocket receive error from peer %s: %v", c.ip, err)
			}
			break out
		}
		// Attempt to unmarshal the request. Only requests that successfully decode
		// will be accepted by the server, though failure to decode does not force
		// a disconnect.
		msg := new(msgjson.Message)
		err = json.Unmarshal(msgBytes, msg)
		if err != nil {
			c.SendError(1, msgjson.NewError(msgjson.RPCParseError,
				"Failed to parse message: "+err.Error()))
			continue
		}
		if msg.ID == 0 {
			c.SendError(1, msgjson.NewError(msgjson.RPCParseError, "request id cannot be zero"))
			continue
		}
		rpcErr := c.handler(msg)
		if rpcErr != nil {
			c.SendError(msg.ID, rpcErr)
		}
	}
}

// pingHandler sends periodic pings to the client.
func (c *WSLink) pingHandler(ctx context.Context) {
	defer c.wg.Done()
	ticker := time.NewTicker(c.pingPeriod)
	ping := []byte{}
out:
	for {
		// Send any messages ready for send until the quit channel is
		// closed.
		select {
		case <-ticker.C:
			err := c.conn.WriteControl(websocket.PingMessage, ping, time.Now().Add(writeWait))
			if err != nil {
				c.Disconnect()
				// Don't really care what the error is, but log it at debug level.
				log.Debugf("WriteMessage ping error: %v", err)
				break out
			}
		case <-ctx.Done():
			break out
		}
	}

	log.Tracef("Websocket output handler done for peer %s", c.ip)
}

// Off will return true if the link has disconnected.
func (c *WSLink) Off() bool {
	return atomic.LoadUint32(&c.on) == 0
}

// IP is the peer address passed to the constructor.
func (c *WSLink) IP() string {
	return c.ip
}

func NewConnection(w http.ResponseWriter, r *http.Request, readTimeout time.Duration) (Connection, error) {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		var hsErr websocket.HandshakeError
		if errors.As(err, &hsErr) {
			log.Errorf("Unexpected websocket error: %v",
				err)
		}
		http.Error(w, "400 Bad Request.", http.StatusBadRequest)
		return nil, err
	}
	// Configure the pong handler.
	reqAddr := r.RemoteAddr
	ws.SetPongHandler(func(string) error {
		log.Tracef("got pong from %v", reqAddr)
		return ws.SetReadDeadline(time.Now().Add(readTimeout))
	})

	// Do not set an initial read deadline until pinging begins.

	return ws, nil
}
