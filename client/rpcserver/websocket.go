// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

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

const updateWalletRoute = "updatewallet"

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
)

type wsClient struct {
	*ws.WSLink
	mtx      sync.Mutex
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
func (s *RPCServer) handleWS(w http.ResponseWriter, r *http.Request) {
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
func (s *RPCServer) websocketHandler(conn ws.Connection, ip string) {
	log.Debugf("New websocket client %s", ip)
	// Create a new websocket client to handle the new websocket connection
	// and wait for it to shutdown.  Once it has shutdown (and hence
	// disconnected), remove it.
	var cl *wsClient
	cl = newWSClient(ip, conn, func(msg *msgjson.Message) *msgjson.Error {
		return s.handleMessage(cl, msg)
	})
	s.mtx.Lock()
	s.clients[cl.cid] = cl
	s.mtx.Unlock()
	defer func() {
		cl.mtx.Lock()
		if cl.feedLoop != nil {
			cl.feedLoop.Stop()
			cl.feedLoop.WaitForShutdown()
		}
		cl.mtx.Unlock()
		s.mtx.Lock()
		delete(s.clients, cl.cid)
		s.mtx.Unlock()
	}()
	cm := dex.NewConnectionMaster(cl)
	err := cm.Connect(s.ctx)
	if err != nil {
		log.Errorf("websocketHandler client Connect: %v")
		return
	}
	cm.Wait()
	log.Tracef("Disconnected websocket client %s", ip)
}

// handleMessage handles the websocket message, calling the right handler for
// the route.
func (s *RPCServer) handleMessage(conn *wsClient, msg *msgjson.Message) *msgjson.Error {
	log.Tracef("message of type %d received for route %s", msg.Type, msg.Route)
	if msg.Type == msgjson.Request {
		handler, found := wsHandlers[msg.Route]
		if !found {
			// If a this request exists in routes, call it.
			if _, found = routes[msg.Route]; !found {
				return msgjson.NewError(msgjson.RPCUnknownRoute, "unknown route '"+msg.Route+"'")
			}
			handler = wsHandleRequest
		}
		return handler(s, conn, msg)
	}
	// Web server doesn't send requests, only responses and notifications, so
	// a response-type message from a client is an error.
	return msgjson.NewError(msgjson.UnknownMessageType, "web server only handles requests")
}

// wsHandlers is the map used by the server to locate the router handler for a
// request.
var wsHandlers = map[string]func(*RPCServer, *wsClient, *msgjson.Message) *msgjson.Error{
	"loadmarket": wsLoadMarket,
	"unmarket":   wsUnmarket,
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
	Book         *core.OrderBook `json:"book"`
	Market       string          `json:"market"`
	Host         string          `json:"host"`
	Base         uint32          `json:"base"`
	BaseSymbol   string          `json:"baseSymbol"`
	Quote        uint32          `json:"quote"`
	QuoteSymbol  string          `json:"quoteSymbol"`
	BaseBalance  uint64          `json:"baseBalance"`
	QuoteBalance uint64          `json:"quoteBalance"`
}

// notify sends a notification to the websocket client.
func (s *RPCServer) notify(route string, payload interface{}) {
	msg, err := msgjson.NewNotification(route, payload)
	if err != nil {
		log.Errorf("notification encoding error: %v", err)
		return
	}
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	for _, cl := range s.clients {
		cl.Send(msg)
	}
}

func (s *RPCServer) notifyWalletUpdate(assetID uint32) {
	walletUpdate := s.core.WalletState(assetID)
	s.notify(updateWalletRoute, walletUpdate)
}

// wsHandleRequest handles requests found in the routes map for a websocket client.
func wsHandleRequest(s *RPCServer, cl *wsClient, msg *msgjson.Message) *msgjson.Error {
	handler := routes[msg.Route]
	params := new(RawParams)
	err := msg.Unmarshal(params)
	if err != nil {
		log.Debugf("cannot unmarshal params for route %s", msg.Route)
		msgError := msgjson.NewError(msgjson.RPCParseError, "unable to unmarshal request")
		return msgError
	}
	payload := handler(s, params)
	// clear all password data in unmarshalled request params after request is handled
	for _, pass := range params.PWArgs {
		pass.Clear()
	}
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		err := fmt.Errorf("unable to encode payload: %v", err)
		panic(err)
	}
	res := &msgjson.Message{
		ID:      msg.ID,
		Type:    msgjson.Response,
		Payload: encodedPayload,
	}
	cl.Send(res)
	return payload.Error
}

// wsLoadMarket is the handler for the 'loadmarket' websocket endpoint.
// Subscribes the client to the notification feed and sends the order book.
func wsLoadMarket(s *RPCServer, cl *wsClient, msg *msgjson.Message) *msgjson.Error {
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
		cl.feedLoop.WaitForShutdown()
	}
	cl.feedLoop = newMarketSyncer(s.ctx, cl, feed)
	cl.mtx.Unlock()

	note, err := msgjson.NewNotification(core.FreshBookAction, &marketResponse{
		Book:  book,
		Base:  market.Base,
		Quote: market.Quote,
	})
	if err != nil {
		log.Errorf("error encoding loadmarkets response: %v", err)
		return msgjson.NewError(msgjson.RPCInternal, "error encoding order book: "+err.Error())
	}
	cl.Send(note)
	return nil
}

// wsUnmarket is the handler for the 'unmarket' websocket endpoint. This empty
// message is sent when the user leaves the markets page. This closes the feed,
// and potentially unsubscribes from orderbook with the server if there are no
// other consumers.
func wsUnmarket(_ *RPCServer, cl *wsClient, _ *msgjson.Message) *msgjson.Error {
	cl.mtx.Lock()
	defer cl.mtx.Unlock()
	if cl.feedLoop != nil {
		cl.feedLoop.Stop()
		cl.feedLoop.WaitForShutdown()
		cl.feedLoop = nil
	}
	return nil
}
