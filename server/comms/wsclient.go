// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package comms

import (
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/decred/dcrdex/server/comms/rpc"
	"github.com/gorilla/websocket"
)

// outBufferSize is the size of the client's buffered channel for outgoing
// messages.
const outBufferSize = 128

// wsConnection represents a communications pathway to the client. In practice,
// it is satisfied by *websocket.Conn. For testing, a stub can be used.
type wsConnection interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	Close() error
}

// When the DEX sends a request to the client, a responseHandler is created
// to wait for the response.
type responseHandler struct {
	expiration time.Time
	f          func(*RPCClient, *rpc.Message)
}

// RPCClient is the local, per-connection representation of a DEX client.
type RPCClient struct {
	// The id is the unique identifier assigned to this client.
	id uint64
	// ip is the client's IP address.
	ip string
	// conn is the gorilla websocket.Conn, or a stub for testing.
	conn wsConnection
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
	// For DEX-originating requests, a the response handler is mapped to the
	// resquest ID.
	reqMtx       sync.Mutex
	respHandlers map[uint64]*responseHandler
	// Upon closing, the client's IP address will be quarantined by the server if
	// ban = true.
	ban bool
}

// newRPCClient is a constructor for a new RPCClient.
func newRPCClient(addr string, conn wsConnection) *RPCClient {
	return &RPCClient{
		on:           true,
		ip:           addr,
		conn:         conn,
		quit:         make(chan struct{}),
		outChan:      make(chan []byte, outBufferSize),
		respHandlers: make(map[uint64]*responseHandler),
	}
}

// Send sends the passed Message to the websocket client. If the client's
// channel if blocking (outBufferSize pending messages), the client is
// disconnected.
func (c *RPCClient) Send(msg *rpc.Message) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	select {
	case c.outChan <- b:
	default:
		log.Warnf("RPCClient outgoing message channel is blocking. disconnecting")
		c.disconnect()
	}
	return nil
}

// Request sends the message to the client and tracks the response handler.
func (c *RPCClient) Request(msg *rpc.Message, f func(conn *RPCClient, msg *rpc.Message)) error {
	c.logReq(msg.ID, f)
	return c.Send(msg)
}

// banish sets the ban flag and closes the client.
func (c *RPCClient) Banish() {
	c.ban = true
	c.disconnect()
}

// sendError sends the rpc.Error to the client.
func (c *RPCClient) sendError(id uint64, rpcErr *rpc.Error) {
	msg, err := rpc.NewResponse(id, nil, rpcErr)
	if err != nil {
		log.Errorf("sendError: failed to create message: %v", err)
	}
	err = c.Send(msg)
	if err != nil {
		// Something is terribly wrong if rpc.Message is not marshalling.
		log.Debug("sendError: failed to send message to %s: %v", c.ip, err)
	}
}

// start begins processing input and output messages.
func (c *RPCClient) start() {
	log.Tracef("Starting websocket client %s", c.ip)

	// Start processing input and output.
	c.wg.Add(2)
	go c.inHandler()
	go c.outHandler()
}

// disconnect closes both the underlying websocket connection and the quit
// channel.
func (c *RPCClient) disconnect() {
	c.quitMtx.Lock()
	defer c.quitMtx.Unlock()
	if !c.on {
		return
	}
	c.on = false
	c.conn.Close()
	close(c.quit)
}

// waitForShutdown blocks until the websocket client goroutines are stopped
// and the connection is closed.
func (c *RPCClient) waitForShutdown() {
	c.wg.Wait()
}

// inHandler handles all incoming messages for the websocket connection. It must
// be run as a goroutine.
func (c *RPCClient) inHandler() {
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
		msg := new(rpc.Message)
		err = json.Unmarshal(msgBytes, &msg)
		if err != nil {
			c.sendError(0, rpc.NewError(rpc.RPCParseError,
				"Failed to parse message: "+err.Error()))
			continue
		}
		switch msg.Type {
		case rpc.Request:
			if msg.ID == 0 {
				c.sendError(0, rpc.NewError(rpc.RPCParseError, "request id cannot be zero"))
				break
			}
			// Look for a registered handler. Failure to find a handler results in an
			// error response but not a disconnect.
			handler, found := rpcRoutes[msg.Route]
			if !found {
				c.sendError(msg.ID, rpc.NewError(rpc.RPCUnknownRoute,
					"unknown route "+msg.Route))
				continue
			}
			// Handle the request.
			rpcError := handler(c, msg)
			if rpcError != nil {
				c.sendError(msg.ID, rpcError)
				continue
			}
		case rpc.Response:
			if msg.ID == 0 {
				c.sendError(0, rpc.NewError(rpc.RPCParseError, "response id cannot be 0"))
				continue
			}
			cb := c.respHandler(msg.ID)
			if cb == nil {
				c.sendError(msg.ID, rpc.NewError(rpc.UnknownResponseID,
					"unkown response ID"))
				continue
			}
			cb.f(c, msg)
		}
	}
	// Ensure the connection is closed.
	c.disconnect()
	c.wg.Done()
}

// outHandler handles all outgoing messages for the websocket connection.
// It uses a buffered channel to serialize output messages while allowing the
// sender to continue running asynchronously.  It must be run as a goroutine.
func (c *RPCClient) outHandler() {
	ticker := time.NewTicker(pingPeriod)
	ping := []byte{}
out:
	for {
		// Send any messages ready for send until the quit channel is
		// closed.
		select {
		case b := <-c.outChan:
			err := c.conn.WriteMessage(websocket.TextMessage, b)
			if err != nil {
				c.disconnect()
				break out
			}
		case <-ticker.C:
			err := c.conn.WriteMessage(websocket.PingMessage, ping)
			if err != nil {
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

// logReq stores the response handler in the respHandlers map. Requests to the
// client are associated with a response handler.
func (c *RPCClient) logReq(id uint64, respHandler func(*RPCClient, *rpc.Message)) {
	c.reqMtx.Lock()
	defer c.reqMtx.Unlock()
	expired := make([]uint64, 0)
	for id, cb := range c.respHandlers {
		if time.Until(cb.expiration) < 0 {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		delete(c.respHandlers, id)
	}
	c.respHandlers[id] = &responseHandler{
		expiration: time.Now().Add(time.Minute * 5),
		f:          respHandler,
	}
}

// respHandler gets the response handler for the provided request ID if it
// exists, else nil.
func (c *RPCClient) respHandler(id uint64) *responseHandler {
	c.reqMtx.Lock()
	defer c.reqMtx.Unlock()
	cb, ok := c.respHandlers[id]
	if ok {
		delete(c.respHandlers, id)
	}
	return cb
}
