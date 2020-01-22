// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package comms

import (
	"sync"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/ws"
)

// outBufferSize is the size of the client's buffered channel for outgoing
// messages.
const outBufferSize = 128

// Link is an interface for a communication channel with an API client. The
// reference implementation of a Link-satisfying type is the wsLink, which
// passes messages over a websocket connection.
type Link interface {
	// ID will return a unique ID by which this connection can be identified.
	ID() uint64
	// Send sends the msgjson.Message to the client.
	Send(msg *msgjson.Message) error
	// Request sends the Request-type msgjson.Message to the client and registers
	// a handler for the response.
	Request(msg *msgjson.Message, f func(Link, *msgjson.Message), expireTime time.Duration, expire func()) error
	// Banish closes the link and quarantines the client.
	Banish()
}

// When the DEX sends a request to the client, a responseHandler is created
// to wait for the response.
type responseHandler struct {
	f      func(Link, *msgjson.Message)
	expire *time.Timer
}

// wsLink is the local, per-connection representation of a DEX client.
type wsLink struct {
	*ws.WSLink
	// The id is the unique identifier assigned to this client.
	id uint64
	// For DEX-originating requests, the response handler is mapped to the
	// resquest ID.
	reqMtx       sync.Mutex
	respHandlers map[uint64]*responseHandler
	// Upon closing, the client's IP address will be quarantined by the server if
	// ban = true.
	ban bool
}

// newWSLink is a constructor for a new wsLink.
func newWSLink(addr string, conn ws.Connection) *wsLink {
	var c *wsLink
	c = &wsLink{
		WSLink: ws.NewWSLink(addr, conn, pingPeriod, func(msg *msgjson.Message) *msgjson.Error {
			return handleMessage(c, msg)
		}),
		respHandlers: make(map[uint64]*responseHandler),
	}
	return c
}

// Banish sets the ban flag and closes the client.
func (c *wsLink) Banish() {
	c.ban = true
	c.Disconnect()
}

func (c *wsLink) ID() uint64 {
	return c.id
}

func handleMessage(c *wsLink, msg *msgjson.Message) *msgjson.Error {
	switch msg.Type {
	case msgjson.Request:
		if msg.ID == 0 {
			return msgjson.NewError(msgjson.RPCParseError, "request id cannot be zero")
		}
		// Look for a registered handler. Failure to find a handler results in an
		// error response but not a disconnect.
		handler := RouteHandler(msg.Route)
		if handler == nil {
			return msgjson.NewError(msgjson.RPCUnknownRoute, "unknown route "+msg.Route)
		}
		// Handle the request.
		rpcError := handler(c, msg)
		if rpcError != nil {
			return rpcError
		}
		return nil
	case msgjson.Response:
		if msg.ID == 0 {
			return msgjson.NewError(msgjson.RPCParseError, "response id cannot be 0")
		}
		cb := c.respHandler(msg.ID)
		if cb == nil {
			return msgjson.NewError(msgjson.UnknownResponseID,
				"unknown response ID")
		}
		cb.f(c, msg)
		return nil
	}
	return msgjson.NewError(msgjson.UnknownMessageType, "unknown message type")
}

func (c *wsLink) expire(id uint64) bool {
	c.reqMtx.Lock()
	defer c.reqMtx.Unlock()
	_, removed := c.respHandlers[id]
	delete(c.respHandlers, id)
	return removed
}

// logReq stores the response handler in the respHandlers map. Requests to the
// client are associated with a response handler.
func (c *wsLink) logReq(id uint64, respHandler func(Link, *msgjson.Message), expireTime time.Duration, expire func()) {
	c.reqMtx.Lock()
	defer c.reqMtx.Unlock()
	doExpire := func() {
		// Delete the response handler, and call the provided expire function if
		// (*wsLink).respHandler has not already retrieved the handler function
		// for execution.
		if c.expire(id) {
			expire()
		}
	}
	c.respHandlers[id] = &responseHandler{
		f:      respHandler,
		expire: time.AfterFunc(expireTime, doExpire),
	}
}

// Request sends the message to the client and tracks the response handler.
func (c *wsLink) Request(msg *msgjson.Message, f func(conn Link, msg *msgjson.Message), expireTime time.Duration, expire func()) error {
	c.logReq(msg.ID, f, expireTime, expire)
	return c.Send(msg)
}

// respHandler extracts the response handler for the provided request ID if it
// exists, else nil. If the handler exists, it will be deleted from the map and
// the expire Timer stopped.
func (c *wsLink) respHandler(id uint64) *responseHandler {
	c.reqMtx.Lock()
	defer c.reqMtx.Unlock()
	cb, ok := c.respHandlers[id]
	if ok {
		delete(c.respHandlers, id)
		// Stop the expiration Timer. If the Timer fired after respHandler was
		// called, but we found the response handler in the map, wsLink.expire
		// is waiting for the reqMtx lock and will return false, thus preventing
		// the registered expire func from executing.
		if !cb.expire.Stop() {
			// Drain the Timer channel if Timer had fired as described above.
			<-cb.expire.C
		}
	}
	return cb
}
