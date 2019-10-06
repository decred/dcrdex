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

// When the DEX sends a request to the client, a requestCallback is created
// to wait for the response.
type requestCallback struct {
	expiration time.Time
	f          func(*rpc.Response)
}

// The RPCClient is the local, per-connection representation of a DEX client.
type RPCClient struct {
	// The id is the unique identifier assigned to this client.
	id uint64
	// ip is the client's IP address.
	ip string
	// conn is the gorilla websocket.Conn, or a stub for testing.
	conn wsConnection
	// quitMtx protects the on flag and closing of the quit channel.
	quitMtx sync.RWMutex
	// on is used internally to prevent mutliple Close calls on the unerlying
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
	reqCallbacks map[uint64]*requestCallback
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
		reqCallbacks: make(map[uint64]*requestCallback),
	}
}

// Respond sends a Message-wrapped Response to the client.
func (c *RPCClient) Respond(resp *rpc.Response) error {
	msg, err := resp.Message()
	if err != nil {
		return err
	}
	return c.sendMessage(msg)
}

// Request sends a Message-wrapped request to the client.
func (c *RPCClient) Request(req *rpc.Request, callback func(*rpc.Response)) error {
	msg, err := req.Message()
	if err != nil {
		return err
	}
	c.logReq(req, callback)
	return c.sendMessage(msg)
}

// sendMessage sends the passed message to the websocket client. If the client's
// channel if blocking (outBufferSize pending messages), the client is
// disconnected.
func (c *RPCClient) sendMessage(msg interface{}) error {
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

// sendError sends the rpc.RPCError to the client.
func (c *RPCClient) sendError(id interface{}, jsonErr *rpc.RPCError) {
	reply, err := rpc.NewResponse(id, nil, jsonErr)
	if err != nil {
		log.Errorf("sendError: failed to create rpc.Response: %v", err)
	}
	msg, err := reply.Message()
	if err != nil {
		log.Errorf("sendError: failed to encode rpc.Message: %v", err)
	}
	err = c.sendMessage(msg)
	if err != nil {
		// Something is terribly wrong if rpc.Response is not marshalling.
		log.Debug("sendError: failed to send rpc.Response to %s: %v", c.ip, err)
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
			c.sendError(-1, rpc.NewRPCError(rpc.RPCParseError,
				"Failed to parse message: "+err.Error()))
			continue
		}
		switch msg.Type {
		case rpc.RequestMessage:
			req, rpcErr := decodeReq(msg.Payload)
			if rpcErr != nil {
				c.sendError(-1, rpcErr)
				continue
			}
			rpcErr = checkRPCVersion(req.Jsonrpc)
			if rpcErr != nil {
				c.sendError(-1, rpcErr)
				break out
			}
			// A null ID is allowed by the JSON-RPC protocol, but the DEX doesn't
			// have a use for them, so we'll filter them out here too.
			if !rpc.IsValidIDType(req.ID) {
				c.sendError(-1, rpc.NewRPCError(rpc.RPCParseError, "unknown ID type"))
				break
			}
			if req.ID == nil {
				c.sendError(-1, rpc.NewRPCError(rpc.RPCParseError, "null request IDs unsupported"))
				break
			}
			// Look for a registered handler. Failure to find a handler results in an
			// error response but not a disconnect.
			handler, found := rpcMethods[req.Method]
			if !found {
				c.sendError(req.ID, rpc.NewRPCError(rpc.RPCUnknownMethod,
					"unknown method "+req.Method))
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
				c.sendError(req.ID, rpcError)
				continue
			}
		case rpc.ResponseMessage:
			resp, rpcErr := decodeResp(msg.Payload)
			if rpcErr != nil {
				c.sendError(-1, rpcErr)
				continue
			}
			if resp.ID == nil {
				c.sendError(-1, rpc.NewRPCError(rpc.RPCParseError, "null response ID"))
				continue
			}
			if !rpc.IsValidIDType(*resp.ID) {
				c.sendError(-1, rpc.NewRPCError(rpc.RPCParseError, "unknown ID type"))
				break
			}
			signedID, ok := resp.ID64()
			// Comms pacakge genreates uint64 IDs, so a response with a negative
			// ID would be invalid.
			if !ok || signedID < 0 {
				c.sendError(-1, rpc.NewRPCError(rpc.RPCParseError,
					"non-integer or negative response ID is not valid"))
				continue
			}
			reqID := uint64(signedID)
			cb := c.respFunc(reqID)
			if cb == nil {
				c.sendError(*resp.ID, rpc.NewRPCError(rpc.UnknownResponseID,
					"unkown response ID"))
				continue
			}
			cb.f(resp)
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

// logReq stores the callbacks in the reqCallbacks map. Requests to the client
// are associated with a callback function to handle the response.
func (c *RPCClient) logReq(req *rpc.Request, callback func(*rpc.Response)) {
	c.reqMtx.Lock()
	defer c.reqMtx.Unlock()
	// First, check for expired callbacks.
	signedID, ok := req.ID64()
	if !ok || signedID < 0 { // Comms generates IDs as uint64, so < 0 bad.
		log.Errorf("non-integer or negative request ID")
		return
	}
	reqID := uint64(signedID)
	expired := make([]uint64, 0)
	for id, cb := range c.reqCallbacks {
		if time.Until(cb.expiration) < 0 {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		delete(c.reqCallbacks, id)
	}
	c.reqCallbacks[reqID] = &requestCallback{
		expiration: time.Now().Add(time.Minute * 5),
		f:          callback,
	}
}

// respFunc gets the callback for the provided request ID if it exists,
// else nil.
func (c *RPCClient) respFunc(id uint64) *requestCallback {
	c.reqMtx.Lock()
	defer c.reqMtx.Unlock()
	cb, ok := c.reqCallbacks[id]
	if ok {
		delete(c.reqCallbacks, id)
	}
	return cb
}

// Checks the that RPC version is exactly "2.0".
func checkRPCVersion(version string) *rpc.RPCError {
	// Check the JSON-RPC version. Only 2.0 is allowed. Disconnect on others.
	if version != rpc.JSONRPCVersion {
		return rpc.NewRPCError(rpc.RPCVersionUnsupported,
			`only RPC version "2.0" supported`)
	}
	return nil
}

// decodeReq decodes the JSON bytes into a rpc Request.
func decodeReq(reqBytes json.RawMessage) (*rpc.Request, *rpc.RPCError) {
	req := new(rpc.Request)
	err := json.Unmarshal(reqBytes, &req)
	if err != nil {
		return nil, rpc.NewRPCError(rpc.RPCParseError,
			"Failed to parse request: "+err.Error())
	}
	if req == nil {
		return nil, rpc.NewRPCError(rpc.RPCParseError,
			"null request payload")
	}
	return req, nil
}

// decodeResp decodes the JSON bytes into a rpc Response.
func decodeResp(respBytes json.RawMessage) (*rpc.Response, *rpc.RPCError) {
	resp := new(rpc.Response)
	err := json.Unmarshal(respBytes, &resp)
	if err != nil {
		return nil, rpc.NewRPCError(rpc.RPCParseError,
			"Failed to parse request: "+err.Error())
	}
	if resp == nil {
		return nil, rpc.NewRPCError(rpc.RPCParseError,
			"null response payload")
	}
	return resp, nil
}
