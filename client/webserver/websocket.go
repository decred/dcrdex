// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/ws"
)

var (
	// Time allowed to read the next pong message from the peer. The
	// default is intended for production, but leaving as a var instead of const
	// to facilitate testing.
	pongWait = 60 * time.Second
	// Send pings to peer with this period. Must be less than pongWait. The
	// default is intended for production, but leaving as a var instead of const
	// to facilitate testing.
	pingPeriod = (pongWait * 9) / 10
	// A client id counter.
	cidCounter int32
	unbip      = dex.BipIDSymbol
)

type wsClient struct {
	*ws.WSLink
	mtx      sync.RWMutex
	cid      int32
	feedLoop *dex.StartStopWaiter
}

func newWSClient(ip string, conn ws.Connection, hndlr func(msg *msgjson.Message) *msgjson.Error) *wsClient {
	return &wsClient{
		WSLink: ws.NewWSLink(ip, conn, pingPeriod, hndlr),
		cid:    atomic.AddInt32(&cidCounter, 1),
	}
}

// handleWS handles the websocket connection request, creating a ws.Connection
// and a websocketHandler thread.
func (s *WebServer) handleWS(w http.ResponseWriter, r *http.Request) {
	// If the IP address includes a port, remove it.
	ip := r.RemoteAddr
	// If a host:port can be parsed, the IP is only the host portion.
	host, _, err := net.SplitHostPort(ip)
	if err == nil && host != "" {
		ip = host
	}
	wsConn, err := ws.NewConnection(w, r, pongWait)
	if err != nil {
		log.Errorf("ws connection error: %v", err)
		return
	}
	go s.websocketHandler(wsConn, ip)
}

// websocketHandler handles a new websocket client by creating a new wsClient,
// starting it, and blocking until the connection closes. This method should be
// run as a goroutine.
func (s *WebServer) websocketHandler(conn ws.Connection, ip string) {
	log.Debugf("New websocket client %s", ip)
	// Create a new websocket client to handle the new websocket connection
	// and wait for it to shutdown.  Once it has shutdown (and hence
	// disconnected), remove it.
	var cl *wsClient
	cl = newWSClient(ip, conn, func(msg *msgjson.Message) *msgjson.Error {
		return s.handleMessage(cl, msg)
	})

	// Lock the clients map before starting the connection listening so that
	// synchronized map accesses are guaranteed to reflect this connection.
	// Also, ensuring only live connections are in the clients map notify from
	// sending before it is connected.
	s.mtx.Lock()
	cm := dex.NewConnectionMaster(cl)
	err := cm.Connect(s.ctx)
	if err != nil {
		s.mtx.Unlock()
		log.Errorf("websocketHandler client Connect: %v")
		return
	}

	// Add the client to the map only after it is connected so that notify does
	// not attempt to send to non-existent connection.
	s.clients[cl.cid] = cl
	s.mtx.Unlock()

	defer func() {
		cl.mtx.Lock()
		if cl.feedLoop != nil {
			cl.feedLoop.Stop()
		}
		cl.mtx.Unlock()

		s.mtx.Lock()
		delete(s.clients, cl.cid)
		s.mtx.Unlock()
	}()

	cm.Wait()
	log.Tracef("Disconnected websocket client %s", ip)
}

// notify sends a notification to the websocket client.
func (s *WebServer) notify(route string, payload interface{}) {
	msg, err := msgjson.NewNotification(route, payload)
	if err != nil {
		log.Errorf("notification encoding error: %v", err)
		return
	}
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	for _, cl := range s.clients {
		if err = cl.Send(msg); err != nil {
			log.Warnf("Failed to send %v notification to client %v at %v: %v",
				msg.Route, cl.cid, cl.IP(), err)
		}
	}
}

func (s *WebServer) notifyWalletUpdate(assetID uint32) {
	walletUpdate := s.core.WalletState(assetID)
	s.notify(updateWalletRoute, walletUpdate)
}

// handleMessage handles the websocket message, calling the right handler for
// the route.
func (s *WebServer) handleMessage(conn *wsClient, msg *msgjson.Message) *msgjson.Error {
	log.Tracef("message of type %d received for route %s", msg.Type, msg.Route)
	if msg.Type == msgjson.Request {
		handler, found := wsHandlers[msg.Route]
		if !found {
			return msgjson.NewError(msgjson.UnknownMessageType, "unknown route '"+msg.Route+"'")
		}
		return handler(s, conn, msg)
	}
	// Web server doesn't send requests, only responses and notifications, so
	// a response-type message from a client is an error.
	return msgjson.NewError(msgjson.UnknownMessageType, "web server only handles requests")
}

// wsHandlers is the map used by the server to locate the router handler for a
// request.
var wsHandlers = map[string]func(*WebServer, *wsClient, *msgjson.Message) *msgjson.Error{
	"loadmarket": wsLoadMarket,
	"unmarket":   wsUnmarket,
	"acknotes":   wsAckNotes,
}

// marketLoad is sent by websocket clients to subscribe to a market and request
// the order book.
type marketLoad struct {
	Host  string `json:"host"`
	Base  uint32 `json:"base"`
	Quote uint32 `json:"quote"`
}

// marketResponse is the websocket update sent when the client requests a
// market via the 'loadmarket' route.
type marketResponse struct {
	Host  string          `json:"host"`
	Book  *core.OrderBook `json:"book"`
	Base  uint32          `json:"base"`
	Quote uint32          `json:"quote"`
}

// wsLoadMarket is the handler for the 'loadmarket' websocket endpoint.
// Subscribes the client to the notification feed by sends the order book.
func wsLoadMarket(s *WebServer, cl *wsClient, msg *msgjson.Message) *msgjson.Error {
	market := new(marketLoad)
	err := json.Unmarshal(msg.Payload, market)
	if err != nil {
		errMsg := fmt.Sprintf("error unmarshaling marketload payload: %v", err)
		log.Errorf(errMsg)
		return msgjson.NewError(msgjson.RPCInternal, errMsg)
	}

	book, feed, err := s.core.Sync(market.Host, market.Base, market.Quote)
	if err != nil {
		errMsg := fmt.Sprintf("error getting order feed: %v", err)
		log.Errorf(errMsg)
		return msgjson.NewError(msgjson.RPCInternal, errMsg)
	}

	cl.mtx.Lock()
	if cl.feedLoop != nil {
		cl.feedLoop.Stop()
	}
	cl.feedLoop = newMarketSyncer(s.ctx, cl, feed)
	cl.mtx.Unlock()

	note, err := msgjson.NewNotification("book", &marketResponse{
		Host:  market.Host,
		Book:  book,
		Base:  market.Base,
		Quote: market.Quote,
	})
	if err != nil {
		log.Errorf("error encoding loadmarkets response: %v", err)
		return msgjson.NewError(msgjson.RPCInternal, "error encoding order book: "+err.Error())
	}
	if err = cl.Send(note); err != nil {
		log.Warnf("Failed to send %v notification to client %v at %v: %v",
			note.Route, cl.cid, cl.IP(), err)
	}
	return nil
}

// wsUnmarket is the handler for the 'unmarket' websocket endpoint. This empty
// message is sent when the user leaves the markets page. This closes the feed,
// and potentially unsubscribes from orderbook with the server if there are no
// other consumers
func wsUnmarket(_ *WebServer, cl *wsClient, _ *msgjson.Message) *msgjson.Error {
	cl.mtx.Lock()
	defer cl.mtx.Unlock()
	if cl.feedLoop != nil {
		cl.feedLoop.Stop()
		cl.feedLoop = nil
	}
	return nil
}

type ackNoteIDs []dex.Bytes

// wsAckNotes is the handler for the 'acknotes' websocket endpoint. Informs the
// Core that the user has seen the specified notifications.
func wsAckNotes(s *WebServer, _ *wsClient, msg *msgjson.Message) *msgjson.Error {
	ids := make(ackNoteIDs, 0)
	err := msg.Unmarshal(&ids)
	if err != nil {
		log.Errorf("error acking notifications: %v", err)
		return nil
	}
	s.core.AckNotes(ids)
	return nil
}
