// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package comms

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

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

// The RPCClient is the local, per-connection representation of a DEX client.
type RPCClient struct {
	// The id is the unique identifier assigned to this client.
	id uint64
	// ip is the client's IP address.
	ip string
	// conn is the gorilla websocket.Conn, or a stub for testing.
	conn wsConnection
	// quitMtx protects the on flag and closing of the quit channel. The quit
	// channel is closed with (*RPCClient).Close only if on = true.
	quitMtx sync.RWMutex
	on      bool
	quit    chan struct{}
	// wg is the client's WaitGroup. The client has at least 3 goroutines, one for
	// read, one for write, and one server goroutine to monitor the client
	// disconnect. The WaitGroup is used to synchronize cleanup on disconnection.
	wg sync.WaitGroup
	// Messages to the client are routed through the outChan. This ensures
	// messages are sent in the correct order, and satisfies the thread-safety
	// requirements of the (*websocket.Conn).WriteMessage.
	outChan chan []byte
	// The ban flag will be set if the DEX requests. Upon closing, the client's
	// IP address will be quarantined by the server if ban = true.
	ban bool
}

// newRPCClient is a constructor for a new RPCClient.
func newRPCClient(addr string, conn wsConnection) *RPCClient {
	return &RPCClient{
		on:      true,
		ip:      addr,
		conn:    conn,
		quit:    make(chan struct{}),
		outChan: make(chan []byte, outBufferSize),
	}
}

// SendMessage sends the passed response to the websocket client.  It is backed
// by a buffered channel, so it will not block until the send channel is full.
// This approach allows a limit to the number of outstanding requests a client
// can make without preventing or blocking on async notifications. SendMessage
// will attempt to marshal the response first, and will return an error if not
// sucessful.
func (c *RPCClient) SendMessage(msg interface{}) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.outChan <- b
	return nil
}

// start begins processing input and output messages.
func (c *RPCClient) start() {
	log.Tracef("Starting websocket client %s", c.ip)

	// Start processing input and output.
	c.wg.Add(2)
	go c.inHandler()
	go c.outHandler()
}

// disconnect closes the underlying websocket connection.
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

// sendError sends the rpc.RPCError to the client.
func (c *RPCClient) sendError(id interface{}, jsonErr *rpc.RPCError) error {
	reply, err := rpc.NewResponse(id, nil, jsonErr)
	if err != nil {
		return fmt.Errorf("Failed to marshal reply: %v", err)
	}
	err = c.SendMessage(reply)
	if err != nil {
		// Something is terribly wrong if rpc.Response is not marshalling.
		return fmt.Errorf("Failed to marshal Response: %v", err)
	}
	return nil
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
		_, msg, err := c.conn.ReadMessage()
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
		req := new(rpc.Request)
		err = json.Unmarshal(msg, &req)
		if err != nil {
			err = c.sendError(-1, &rpc.RPCError{
				Code:    rpc.RPCParseError,
				Message: "Failed to parse request: " + err.Error(),
			})
			if err != nil {
				log.Errorf("sendError error while sending json parse error: %v", err)
				break out
			}
			continue
		}
		// Check the JSON-RPC version. Only 2.0 is allowed. Disconnect on others.
		if req.Jsonrpc != rpc.JSONRPCVersion {
			err = c.sendError(-1, &rpc.RPCError{
				Code:    rpc.RPCVersionUnsupported,
				Message: `only RPC version "2.0" supported`,
			})
			if err != nil {
				log.Errorf("sendError error while sending json version error: %v", err)
			}
			break out
		}
		// Look for a registered handler. Failure to find a handler results in an
		// error response but not a disconnect.
		handler, found := rpcMethods[req.Method]
		if !found {
			err = c.sendError(req.ID, &rpc.RPCError{
				Code:    rpc.RPCUnknownMethod,
				Message: "unknown method " + req.Method,
			})
			if err != nil {
				log.Errorf("sendError error while sending unknown method error: %v", err)
				break out
			}
			continue
		}
		// Handle the request.
		rpcError := handler(c, req)
		if rpcError != nil {
			// The server can request a quarantine by returning the
			// RPCQuarantineClient code.
			if rpcError.Code == rpc.RPCQuarantineClient {
				c.ban = true
				break out
			}
			err = c.sendError(req.ID, rpcError)
			if err != nil {
				log.Error("sendError error while sending rpc error: %v", err)
				break out
			}
			continue
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
