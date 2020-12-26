// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package comms

import (
	"sync"
	"time"

	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/ws"
)

const readLimitAuthorized = 65536

// criticalRoutes are not subject to the rate limiter on websocket connections.
var criticalRoutes = map[string]bool{
	msgjson.ConfigRoute: true,
}

// Link is an interface for a communication channel with an API client. The
// reference implementation of a Link-satisfying type is the wsLink, which
// passes messages over a websocket connection.
type Link interface {
	// Done returns a channel that is closed when the link goes down.
	Done() <-chan struct{}
	// ID returns a unique ID by which this connection can be identified.
	ID() uint64
	// Addr returns the string-encoded IP address.
	Addr() string
	// Send sends the msgjson.Message to the peer.
	Send(msg *msgjson.Message) error
	// SendError sends the msgjson.Error to the peer, with reference to a
	// request message ID.
	SendError(id uint64, rpcErr *msgjson.Error)
	// Request sends the Request-type msgjson.Message to the client and registers
	// a handler for the response.
	Request(msg *msgjson.Message, f func(Link, *msgjson.Message), expireTime time.Duration, expire func()) error
	// Banish closes the link and quarantines the client.
	Banish()
	// Disconnect closes the link.
	Disconnect()
	// Authorized should be called from a request handler when the connection
	// becomes authorized. Request handlers must be run synchronous with other
	// reads or it will be a data race with the link's input loop.
	Authorized()
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
	// meter is a function that will be checked to see if certain data API
	// requests should be denied due to rate limits or if the API disabled.
	meter func() (int, error)
}

// newWSLink is a constructor for a new wsLink.
func newWSLink(addr string, conn ws.Connection, limitRate func() (int, error)) *wsLink {
	var c *wsLink
	c = &wsLink{
		WSLink: ws.NewWSLink(addr, conn, pingPeriod, func(msg *msgjson.Message) *msgjson.Error {
			return handleMessage(c, msg)
		}, log.SubLogger("WS")),
		respHandlers: make(map[uint64]*responseHandler),
		meter:        limitRate,
	}
	return c
}

// Banish sets the ban flag and closes the client.
func (c *wsLink) Banish() {
	c.ban = true
	c.Disconnect()
}

// ID returns a unique ID by which this connection can be identified.
func (c *wsLink) ID() uint64 {
	return c.id
}

// Addr returns the string-encoded IP address.
func (c *wsLink) Addr() string {
	return c.WSLink.Addr()
}

// Authorized should be called from a request handler when the connection
// becomes authorized. Unless it is run in a request handler synchronous with
// other reads or prior to starting the link, it will be a data race with the
// link's input loop. dex/ws.(*WsLink).inHandler does not run request handlers
// concurrently with reads.
func (c *wsLink) Authorized() {
	c.SetReadLimit(readLimitAuthorized)
}

// The WSLink.handler for WSLink.inHandler
func handleMessage(c *wsLink, msg *msgjson.Message) *msgjson.Error {
	switch msg.Type {
	case msgjson.Request:
		if msg.ID == 0 {
			return msgjson.NewError(msgjson.RPCParseError, "request id cannot be zero")
		}
		// Look for a registered route handler.
		handler := RouteHandler(msg.Route)
		if handler != nil {
			// Handle the request.
			return handler(c, msg)
		}

		// Look for an HTTP handler.
		httpHandler := httpRoutes[msg.Route]
		if httpHandler == nil {
			return msgjson.NewError(msgjson.RPCUnknownRoute, "unknown route")
		}

		// If it's not a critical route, check the rate limiters.
		if !criticalRoutes[msg.Route] {
			if _, err := c.meter(); err != nil {
				// These errors are actually formatted nicely for sending, since
				// they are used directly in HTTP errors as well.
				return msgjson.NewError(msgjson.RouteUnavailableError, err.Error())
			}
		}

		// Prepare the thing and unmarshal.
		var thing interface{}
		switch msg.Route {
		case msgjson.CandlesRoute:
			thing = new(msgjson.CandlesRequest)
		case msgjson.OrderBookRoute:
			thing = new(msgjson.OrderBookSubscription)
		}
		if thing != nil {
			err := msg.Unmarshal(thing)
			if err != nil {
				return msgjson.NewError(msgjson.RPCParseError, "json parse error")
			}
		}

		// Process request.
		resp, err := httpHandler(thing)
		if err != nil {
			return msgjson.NewError(msgjson.HTTPRouteError, err.Error())
		}

		// Respond.
		msg, err := msgjson.NewResponse(msg.ID, resp, nil)
		if err == nil {
			err = c.Send(msg)
		}

		if err != nil {
			log.Errorf("Error sending response to %s for requested route %q: %v", c.Addr(), msg.Route, err)
		}
		return nil

	case msgjson.Response:
		// NOTE: In the event of an error, we respond to a response, which makes
		// no sense. A new mechanism is needed with appropriate client handling.
		if msg.ID == 0 {
			return msgjson.NewError(msgjson.RPCParseError, "response id cannot be 0")
		}
		cb := c.respHandler(msg.ID)
		if cb == nil {
			log.Debugf("comms.handleMessage: handler for msg ID %d not found", msg.ID)
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

// Request sends the message to the client and tracks the response handler. If
// the response handler is called, it is guaranteed that the request Message.ID
// is equal to the response Message.ID passed to the handler (see the
// msgjson.Response case in handleMessage).
func (c *wsLink) Request(msg *msgjson.Message, f func(conn Link, msg *msgjson.Message), expireTime time.Duration, expire func()) error {
	// log.Tracef("Registering '%s' request ID %d (wsLink)", msg.Route, msg.ID)
	c.logReq(msg.ID, f, expireTime, expire)
	// Send errors are (1) connection is already down or (2) json marshal
	// failure. Any connection write errors just cause the link to quit as the
	// goroutine that actually does the write does not relay any errors back to
	// the caller. The request will eventually expire when no response comes.
	// This is not ideal - we may consider an error callback, or different
	// Send/SendNow/QueueSend functions.
	err := c.Send(msg)
	if err != nil {
		// Neither expire nor the handler should run. Stop the expire timer
		// created by logReq and delete the response handler it added. The
		// caller receives a non-nil error to deal with it.
		log.Debugf("(*wsLink).Request(route '%s') Send error, unregistering msg ID %d handler: %v",
			msg.Route, msg.ID, err)
		c.respHandler(msg.ID) // drop the removed responseHandler
	}
	return err
}

// respHandler extracts the response handler for the provided request ID if it
// exists, else nil. If the handler exists, it will be deleted from the map and
// the expire Timer stopped.
func (c *wsLink) respHandler(id uint64) *responseHandler {
	c.reqMtx.Lock()
	defer c.reqMtx.Unlock()
	cb, ok := c.respHandlers[id]
	if ok {
		// Stop the expiration Timer. If the Timer fired after respHandler was
		// called, but we found the response handler in the map, wsLink.expire
		// is waiting for the reqMtx lock and will return false, thus preventing
		// the registered expire func from executing.
		cb.expire.Stop()
		delete(c.respHandlers, id)
	}
	return cb
}
