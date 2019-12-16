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
	Request(msg *msgjson.Message, f func(Link, *msgjson.Message)) error
	// Banish closes the link and quarantines the client.
	Banish()
}

// When the DEX sends a request to the client, a responseHandler is created
// to wait for the response.
type responseHandler struct {
	expiration time.Time
	f          func(Link, *msgjson.Message)
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
		// Look for a registered handler. Failure to find a handler results in an
		// error response but not a disconnect.
		handler := RouteHandler(msg.Route)
		if handler == nil {
			return msgjson.NewError(msgjson.RPCUnknownRoute, "unknown route "+msg.Route)
		}
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
		}
	}
	return msgjson.NewError(msgjson.UnknownMessageType, "unknown message type")
}

// cleanUpExpired cleans up the response handler map.
func (c *wsLink) cleanUpExpired() {
	c.reqMtx.Lock()
	defer c.reqMtx.Unlock()
	var expired []uint64
	for id, cb := range c.respHandlers {
		if time.Until(cb.expiration) < 0 {
			expired = append(expired, id)
		}
	}
	for _, id := range expired {
		delete(c.respHandlers, id)
	}
}

// logReq stores the response handler in the respHandlers map. Requests to the
// client are associated with a response handler.
func (c *wsLink) logReq(id uint64, respHandler func(Link, *msgjson.Message)) {
	c.reqMtx.Lock()
	defer c.reqMtx.Unlock()
	c.respHandlers[id] = &responseHandler{
		expiration: time.Now().Add(time.Minute * 5),
		f:          respHandler,
	}
	// clean up the response map.
	if len(c.respHandlers) > 1 {
		go c.cleanUpExpired()
	}
}

// Request sends the message to the client and tracks the response handler.
func (c *wsLink) Request(msg *msgjson.Message, f func(conn Link, msg *msgjson.Message)) error {
	c.logReq(msg.ID, f)
	return c.Send(msg)
}

// respHandler extracts the response handler for the provided request ID if it
// exists, else nil. If the handler exists, it will be deleted from the map.
func (c *wsLink) respHandler(id uint64) *responseHandler {
	c.reqMtx.Lock()
	defer c.reqMtx.Unlock()
	cb, ok := c.respHandlers[id]
	if ok {
		delete(c.respHandlers, id)
	}
	return cb
}
