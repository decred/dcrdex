// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package websocket

import (
	"context"
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

// updateWalletRoute is a notification route that updates the state of a wallet.
const updateWalletRoute = "update_wallet"

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

// Core specifies the needed methods for Server to operate. Satisfied by *core.Core.
type Core interface {
	WalletState(assetID uint32) *core.WalletState
	SyncBook(dex string, base, quote uint32) (*core.OrderBook, *core.BookFeed, error)
	AckNotes([]dex.Bytes)
}

// Server contains fields used by the websocket server.
type Server struct {
	core       Core
	log        dex.Logger
	mSyncerLog dex.Logger
	ctx        context.Context
	mtx        sync.RWMutex
	clients    map[int32]*wsClient
}

// New returns a new websocket Server.
func New(ctx context.Context, core Core, log dex.Logger) *Server {
	return &Server{
		core:       core,
		log:        log,
		mSyncerLog: log.SubLogger("MSYNC"),
		ctx:        ctx,
		clients:    make(map[int32]*wsClient),
	}
}

// Set the server's context. Must be called before using and never during use.
func (s *Server) SetContext(ctx context.Context) {
	s.ctx = ctx
}

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

// Shutdown disconnects all connected clients.
func (s *Server) Shutdown() {
	s.mtx.Lock()
	for _, cl := range s.clients {
		cl.Disconnect()
	}
	s.mtx.Unlock()
}

// HandleConnect handles the websocket connection request, creating a ws.Connection
// and a connect thread.
func (s *Server) HandleConnect(w http.ResponseWriter, r *http.Request) {
	// If the IP address includes a port, remove it.
	ip := r.RemoteAddr
	// If a host:port can be parsed, the IP is only the host portion.
	host, _, err := net.SplitHostPort(ip)
	if err == nil && host != "" {
		ip = host
	}
	wsConn, err := ws.NewConnection(w, r, pongWait)
	if err != nil {
		s.log.Errorf("ws connection error: %v", err)
		return
	}
	go s.connect(wsConn, ip)
}

// connect handles a new websocket client by creating a new wsClient, starting
// it, and blocking until the connection closes. This method should be
// run as a goroutine.
func (s *Server) connect(conn ws.Connection, ip string) {
	s.log.Debugf("New websocket client %s", ip)
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
		s.log.Errorf("websocketHandler client Connect: %v")
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
			cl.feedLoop.WaitForShutdown()
		}
		cl.mtx.Unlock()

		s.mtx.Lock()
		delete(s.clients, cl.cid)
		s.mtx.Unlock()
	}()

	cm.Wait()
	s.log.Tracef("Disconnected websocket client %s", ip)
}

// Notify sends a notification to the websocket client.
func (s *Server) Notify(route string, payload interface{}) {
	msg, err := msgjson.NewNotification(route, payload)
	if err != nil {
		s.log.Errorf("notification encoding error: %v", err)
		return
	}
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	for _, cl := range s.clients {
		if err = cl.Send(msg); err != nil {
			s.log.Warnf("Failed to send %v notification to client %v at %v: %v",
				msg.Route, cl.cid, cl.IP(), err)
		}
	}
}

// NotifyWalletUpdate sends a wallet update notification.
func (s *Server) NotifyWalletUpdate(assetID uint32) {
	walletUpdate := s.core.WalletState(assetID)
	s.Notify(updateWalletRoute, walletUpdate)
}

// handleMessage handles the websocket message, calling the right handler for
// the route.
func (s *Server) handleMessage(conn *wsClient, msg *msgjson.Message) *msgjson.Error {
	s.log.Tracef("message of type %d received for route %s", msg.Type, msg.Route)
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

// All request handlers must be defined with this signature.
type wsHandler func(*Server, *wsClient, *msgjson.Message) *msgjson.Error

// wsHandlers is the map used by the server to locate the router handler for a
// request.
var wsHandlers = map[string]wsHandler{
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

// marketSyncer is used to synchronize market subscriptions. The marketSyncer
// manages a map of clients who are subscribed to the market, and distributes
// order book updates when received.
type marketSyncer struct {
	log  dex.Logger
	feed *core.BookFeed
	cl   *wsClient
}

// newMarketSyncer is the constructor for a marketSyncer, returned as a running
// *dex.StartStopWaiter.
func newMarketSyncer(ctx context.Context, cl *wsClient, feed *core.BookFeed, log dex.Logger) *dex.StartStopWaiter {
	ssWaiter := dex.NewStartStopWaiter(&marketSyncer{
		feed: feed,
		cl:   cl,
		log:  log,
	})
	ssWaiter.Start(ctx)
	return ssWaiter
}

// Run starts the marketSyncer listening for BookUpdates, which it relays to the
// websocket client as notifications.
func (m *marketSyncer) Run(ctx context.Context) {
	defer m.feed.Close()
out:
	for {
		select {
		case update, ok := <-m.feed.C:
			if !ok {
				// Should not happen, but don't spin furiously.
				m.log.Warnf("marketSyncer stopping on feed closed")
				return
			}
			if update.Action == core.FreshBookAction {
				// For FreshBookAction, translate the *core.MarketOrderBook
				// payload into a *marketResponse.
				mob, ok := update.Payload.(*core.MarketOrderBook)
				if !ok {
					m.log.Errorf("FreshBookAction payload not a *MarketOrderBook")
					continue
				}
				update.Payload = &marketResponse{
					Host:  update.Host,
					Book:  mob.Book,
					Base:  mob.Base,
					Quote: mob.Quote,
				}
				m.log.Tracef("FreshBookAction: %v", update.MarketID)
			}
			note, err := msgjson.NewNotification(update.Action, update)
			if err != nil {
				m.log.Errorf("error encoding notification message: %v", err)
				break out
			}
			err = m.cl.Send(note)
			if err != nil {
				m.log.Debug("send error. ending market feed: %v", err)
				break out
			}
		case <-ctx.Done():
			break out
		}
	}
}

// wsLoadMarket is the handler for the 'loadmarket' websocket endpoint.
// Subscribes the client to the notification feed and sends the order book.
func wsLoadMarket(s *Server, cl *wsClient, msg *msgjson.Message) *msgjson.Error {
	market := new(marketLoad)
	err := json.Unmarshal(msg.Payload, market)
	if err != nil {
		errMsg := fmt.Sprintf("error unmarshaling marketload payload: %v", err)
		s.log.Errorf(errMsg)
		return msgjson.NewError(msgjson.RPCInternal, errMsg)
	}

	book, feed, err := s.core.SyncBook(market.Host, market.Base, market.Quote)
	if err != nil {
		errMsg := fmt.Sprintf("error getting order feed: %v", err)
		s.log.Errorf(errMsg)
		return msgjson.NewError(msgjson.RPCInternal, errMsg)
	}

	cl.mtx.Lock()
	if cl.feedLoop != nil {
		cl.feedLoop.Stop()
		cl.feedLoop.WaitForShutdown()
	}
	cl.feedLoop = newMarketSyncer(s.ctx, cl, feed, s.mSyncerLog)
	cl.mtx.Unlock()

	note, err := msgjson.NewNotification(core.FreshBookAction, &marketResponse{
		Host:  market.Host,
		Book:  book,
		Base:  market.Base,
		Quote: market.Quote,
	})
	if err != nil {
		s.log.Errorf("error encoding loadmarkets response: %v", err)
		return msgjson.NewError(msgjson.RPCInternal, "error encoding order book: "+err.Error())
	}
	if err = cl.Send(note); err != nil {
		s.log.Warnf("Failed to send %v notification to client %v at %v: %v",
			note.Route, cl.cid, cl.IP(), err)
	}
	return nil
}

// wsUnmarket is the handler for the 'unmarket' websocket endpoint. This empty
// message is sent when the user leaves the markets page. This closes the feed,
// and potentially unsubscribes from orderbook with the server if there are no
// other consumers
func wsUnmarket(_ *Server, cl *wsClient, _ *msgjson.Message) *msgjson.Error {
	cl.mtx.Lock()
	defer cl.mtx.Unlock()
	if cl.feedLoop != nil {
		cl.feedLoop.Stop()
		cl.feedLoop.WaitForShutdown()
		cl.feedLoop = nil
	}
	return nil
}

type ackNoteIDs []dex.Bytes

// wsAckNotes is the handler for the 'acknotes' websocket endpoint. Informs the
// Core that the user has seen the specified notifications.
func wsAckNotes(s *Server, _ *wsClient, msg *msgjson.Message) *msgjson.Error {
	ids := make(ackNoteIDs, 0)
	err := msg.Unmarshal(&ids)
	if err != nil {
		s.log.Errorf("error acking notifications: %v", err)
		return nil
	}
	s.core.AckNotes(ids)
	return nil
}
