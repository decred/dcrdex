// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"github.com/gorilla/websocket"
)

// outBufferSize is the size of the client's buffered channel for outgoing
// messages.
const outBufferSize = 128

// websocket.Upgrader is the preferred method of upgrading a request to a
// websocket connection.
var upgrader = websocket.Upgrader{}

// Error is just a basic error.
type Error string

// Error satisfies the error interface.
func (e Error) Error() string {
	return string(e)
}

// ErrClientDisconnected will be returned if Send or Request is called on a
// disconnected link.
const ErrClientDisconnected = Error("client disconnected")

// Connection represents a websocket connection to the client. In practice,
// it is satisfied by *websocket.Conn. For testing, a stub can be used.
type Connection interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	Close() error
}

// wsLink is the local, per-connection representation of a DEX client.
type WSLink struct {
	// ip is the client's IP address.
	ip string
	// conn is the gorilla websocket.Conn, or a stub for testing.
	conn Connection
	// quitMtx protects the on flag and closing of the quit channel.
	quitMtx sync.RWMutex
	// on is used internally to prevent multiple Close calls on the underlying
	// connections.
	on bool
	// Once the client is disconnected, the quit channel will be closed.
	quit chan struct{}
	// wg is the client's WaitGroup. The client has at least 3 goroutines, one for
	// read, one for write, and one server goroutine to monitor the client
	// disconnect. The WaitGroup is used to synchronize cleanup on disconnection.
	wg sync.WaitGroup
	// Messages to the client are routed through the outChan. This ensures
	// messages are sent in the correct order, and satisfies the thread-safety
	// requirements of the (*websocket.Conn).WriteMessage.
	outChan chan []byte
	// A master message handler.
	handler func(*msgjson.Message) *msgjson.Error
	// pingPeriod is how often to ping the client.
	pingPeriod time.Duration
}

// newWSLink is a constructor for a new WSLink.
func NewWSLink(addr string, conn Connection, pingPeriod time.Duration, handler func(*msgjson.Message) *msgjson.Error) *WSLink {
	return &WSLink{
		on:         true,
		ip:         addr,
		conn:       conn,
		quit:       make(chan struct{}),
		outChan:    make(chan []byte, outBufferSize),
		pingPeriod: pingPeriod,
		handler:    handler,
	}
}

// Send sends the passed Message to the websocket client. If the client's
// channel if blocking (outBufferSize pending messages), the client is
// disconnected.
func (c *WSLink) Send(msg *msgjson.Message) error {
	if c.Off() {
		return ErrClientDisconnected
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	select {
	case c.outChan <- b:
	case <-c.quit:
		return ErrClientDisconnected
	}
	return nil
}

// SendError sends the msgjson.Error to the client.
func (c *WSLink) SendError(id uint64, rpcErr *msgjson.Error) {
	msg, err := msgjson.NewResponse(id, nil, rpcErr)
	if err != nil {
		log.Errorf("SendError: failed to create message: %v", err)
	}
	err = c.Send(msg)
	if err != nil {
		log.Debug("SendError: failed to send message to %s: %v", c.ip, err)
	}
}

// Start begins processing input and output messages.
func (c *WSLink) Start() {
	log.Tracef("Starting websocket client %s", c.ip)
	// Start processing input and output.
	c.wg.Add(2)
	go c.inHandler()
	go c.outHandler()
}

// Disconnect closes both the underlying websocket connection and the quit
// channel.
func (c *WSLink) Disconnect() {
	c.quitMtx.Lock()
	defer c.quitMtx.Unlock()
	if !c.on {
		return
	}
	c.on = false
	c.conn.Close()
	close(c.quit)
}

// WaitForShutdown blocks until the websocket client goroutines are stopped
// and the connection is closed.
func (c *WSLink) WaitForShutdown() {
	c.wg.Wait()
}

// inHandler handles all incoming messages for the websocket connection. It must
// be run as a goroutine.
func (c *WSLink) inHandler() {
out:
	for {
		// Break out of the loop once the quit channel has been closed.
		// Use a non-blocking select here so we fall through otherwise.
		select {
		case <-c.quit:
			break out
		default:
		}
		// Block until a message is received or an error occurs.
		_, msgBytes, err := c.conn.ReadMessage()
		if err != nil {
			// Log the error if it's not due to disconnecting.
			if err != io.EOF {
				log.Errorf("Websocket receive error from %s: %v", c.ip, err)
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
	// Ensure the connection is closed.
	c.Disconnect()
	c.wg.Done()
}

// outHandler handles all outgoing messages for the websocket connection.
// It uses a buffered channel to serialize output messages while allowing the
// sender to continue running asynchronously.  It must be run as a goroutine.
func (c *WSLink) outHandler() {
	ticker := time.NewTicker(c.pingPeriod)
	ping := []byte{}
out:
	for {
		// Send any messages ready for send until the quit channel is
		// closed.
		select {
		case b := <-c.outChan:
			err := c.conn.WriteMessage(websocket.TextMessage, b)
			if err != nil {
				c.Disconnect()
				break out
			}
		case <-ticker.C:
			err := c.conn.WriteMessage(websocket.PingMessage, ping)
			if err != nil {
				c.Disconnect()
				// Don't really care what the error is, but log it at debug level.
				log.Debugf("WriteMessage ping error: %v", err)
				break out
			}
		case <-c.quit:
			break out
		}
	}

	// Drain any wait channels before exiting so nothing is left waiting
	// around to send.
cleanup:
	for {
		select {
		case <-c.outChan:
		default:
			break cleanup
		}
	}
	c.wg.Done()
	log.Tracef("Websocket client output handler done for %s", c.ip)
}

// Off will return true if the client has disconnected.
func (c *WSLink) Off() bool {
	c.quitMtx.RLock()
	defer c.quitMtx.RUnlock()
	return !c.on
}

// IP is the address passed to the constructor.
func (c *WSLink) IP() string {
	return c.ip
}

func NewConnection(w http.ResponseWriter, r *http.Request, wait time.Duration) (Connection, error) {
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
	pongHandler := func(string) error {
		return ws.SetReadDeadline(time.Now().Add(wait))
	}
	err = pongHandler("")
	if err != nil {
		return nil, fmt.Errorf("error setting read deadline: %v", err)
	}
	ws.SetPongHandler(pongHandler)
	return ws, nil
}
