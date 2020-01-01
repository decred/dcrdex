// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"encoding/json"
	"net"
	"net/http"
	"time"

	"decred.org/dcrdex/client/core"
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
)

// handleWS handles the websocket connection request, creating a ws.Connection
// and a websocketHandler thread.
func (s *webServer) handleWS(w http.ResponseWriter, r *http.Request) {
	// If the IP address includes a port, remove it.
	ip := r.RemoteAddr
	// If a host:port can be parsed, the IP is only the host portion.
	host, _, err := net.SplitHostPort(ip)
	if err == nil && host != "" {
		ip = host
	}
	wsConn, err := ws.NewConnection(w, r, pingPeriod+pongWait)
	if err != nil {
		log.Errorf("ws connection error: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	go s.websocketHandler(wsConn, ip)
}

// websocketHandler handles a new websocket client by creating a new wsClient,
// starting it, and blocking until the connection closes. This method should be
// run as a goroutine.
func (s *webServer) websocketHandler(conn ws.Connection, ip string) {
	log.Tracef("New websocket client %s", ip)
	// Create a new websocket client to handle the new websocket connection
	// and wait for it to shutdown.  Once it has shutdown (and hence
	// disconnected), remove it.
	var wsLink *ws.WSLink
	wsLink = ws.NewWSLink(ip, conn, pingPeriod, func(msg *msgjson.Message) *msgjson.Error {
		return s.handleMessage(wsLink, msg)
	})
	wsLink.Start()
	wsLink.WaitForShutdown()
	log.Tracef("Disconnected websocket client %s", ip)
}

// handleMessage handles the websocket message, calling the right handler for
// the route.
func (s *webServer) handleMessage(conn *ws.WSLink, msg *msgjson.Message) *msgjson.Error {
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
var wsHandlers = map[string]func(*webServer, *ws.WSLink, *msgjson.Message) *msgjson.Error{
	"loadmarket": wsLoadMarket,
	"unmarket":   wsUnmarket,
}

// marketLoad is sent by websocket clients to subscribe to a market and request
// the order book.
type marketLoad struct {
	DEX    string `json:"dex"`
	Market string `json:"market"`
}

// marketResponse is the websocket update sent when the client requests a
// market via the 'loadmarket' route.
type marketResponse struct {
	Book         *core.OrderBook `json:"book"`
	Market       string          `json:"market"`
	DEX          string          `json:"dex"`
	Base         string          `json:"base"`
	Quote        string          `json:"quote"`
	BaseBalance  float64         `json:"baseBalance"`
	QuoteBalance float64         `json:"quoteBalance"`
}

// wsLoadMarket is the handler for the 'loadmarket' websocket endpoint. Sets the
// currently monitored markets and starts the notification feed with the order
// book.
func wsLoadMarket(s *webServer, conn *ws.WSLink, msg *msgjson.Message) *msgjson.Error {
	market := new(marketLoad)
	err := json.Unmarshal(msg.Payload, market)
	if err != nil {
		log.Errorf("error unmarshaling marketload payload: %v", err)
		return msgjson.NewError(msgjson.RPCInternal, "error unmarshaling marketload payload: "+err.Error())
	}
	base, quote, err := decodeMarket(market.Market)
	if err != nil {
		return msgjson.NewError(msgjson.UnknownMarketError, err.Error())
	}
	baseBalance, err := s.core.Balance(base)
	if err != nil {
		log.Errorf("unable to get balance for %s", base)
		return msgjson.NewError(msgjson.RPCInternal, "error getting balance for "+base+": "+err.Error())
	}
	quoteBalance, err := s.core.Balance(quote)
	if err != nil {
		log.Errorf("unable to get balance for %s", quote)
		return msgjson.NewError(msgjson.RPCInternal, "error getting balance for "+quote+": "+err.Error())
	}

	// Switch current market.
	book, err := s.watchMarket(market.DEX, market.Market)
	if err != nil {
		log.Errorf("error watching market %s @ %s: %v", market.Market, market.DEX, err)
		return msgjson.NewError(msgjson.RPCInternal, "error watching "+market.Market)
	}

	note, err := msgjson.NewNotification("book", &marketResponse{
		Book:         book,
		Market:       market.Market,
		DEX:          market.DEX,
		Base:         base,
		Quote:        quote,
		BaseBalance:  baseBalance,
		QuoteBalance: quoteBalance,
	})

	if err != nil {
		log.Errorf("error encoding loadmarkets response: %v", err)
		return msgjson.NewError(msgjson.RPCInternal, "error encoding order book: "+err.Error())
	}
	conn.Send(note)
	return nil
}

// wsUnmarket is the handler for the 'unmarket' websocket endpoint. This empty
// message is sent when the user leaves the markets page. Unsubscribes from the
// current market.
func wsUnmarket(s *webServer, conn *ws.WSLink, _ *msgjson.Message) *msgjson.Error {
	s.unmarket()
	return nil
}
