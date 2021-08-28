// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package comms

import (
	"context"
	"crypto/elliptic"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/ws"
	"github.com/decred/dcrd/certgen"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"
)

const (
	// rpcTimeoutSeconds is the number of seconds a connection to the
	// RPC server is allowed to stay open without authenticating before it
	// is closed.
	rpcTimeoutSeconds = 10

	// rpcMaxClients is the maximum number of active websocket connections
	// allowed.
	rpcMaxClients = 10000

	// rpcMaxConnsPerIP is the maximum number of active websocket connections
	// allowed per IP, loopback excluded.
	rpcMaxConnsPerIP = 8

	// banishTime is the default duration of a client quarantine.
	banishTime = time.Hour

	// Per-ip rate limits for market data API routes.
	ipMaxRatePerSec = 1
	ipMaxBurstSize  = 5

	// Per-websocket-connection request limits. Rate should be a reasonable
	// sustained rate, while burst should consider bulk reconnect operations.
	wsRateStatus, wsBurstStatus     = 10, 500     // order_status and match_status (combined)
	wsRateOrder, wsBurstOrder       = 5, 100      // market, limit, and cancel (combined)
	wsRateInfo, wsBurstInfo         = 10, 200     // low-cost route limiter for: config, fee_rate  (combined)
	wsRateBook, wsBurstBook         = 1, 100      // orderbook, assuming 100 markets subscribed max
	wsRateRegister, wsBurstRegister = 1 / 60.0, 1 // register, rate.Every(time.Minute) but const
	// The cumulative rates below would need to be less than sum of above to
	// actually trip unless it is also applied to unspecified routes.
	wsRateTotal, wsBurstTotal = 40, 1000
)

var (
	// Time allowed to read the next pong message from the peer. The default is
	// intended for production, but leaving as a var instead of const to
	// facilitate testing. This is the websocket read timeout set by the pong
	// handler. The first read deadline is set by the ws.WSLink.
	pongWait = 20 * time.Second

	// Send pings to peer with this period. Must be less than pongWait. The
	// default is intended for production, but leaving as a var instead of const
	// to facilitate testing.
	pingPeriod = (pongWait * 9) / 10 // i.e. 18 sec

	// globalHTTPRateLimiter is a limit on the global HTTP request limit. The
	// global rate limiter is like a rudimentary auto-spam filter for
	// non-critical routes, including all routes registered as HTTP routes.
	globalHTTPRateLimiter = rate.NewLimiter(100, 1000) // rate per sec, max burst

	// ipRateLimiter is a per-client rate limiter for the HTTP endpoints
	// requests and httpRoutes (the market data API). The Server manages
	// separate limiters used with the websocket routes, rpcRoutes.
	ipHTTPRateLimiter = make(map[dex.IPKey]*ipRateLimiter)
	rateLimiterMtx    sync.RWMutex
)

var idCounter uint64

// ipRateLimiter is used to track an IPs HTTP request rate.
type ipRateLimiter struct {
	*rate.Limiter
	lastHit time.Time
}

// Get an ipRateLimiter for the IP. Creates a new one if it doesn't exist. This
// is for use with the HTTP endpoints and httpRoutes (the data API), not the
// websocket request routes in rpcRoutes.
func getIPLimiter(ip dex.IPKey) *ipRateLimiter {
	rateLimiterMtx.Lock()
	defer rateLimiterMtx.Unlock()
	limiter := ipHTTPRateLimiter[ip]
	if limiter != nil {
		limiter.lastHit = time.Now()
		return limiter
	}
	limiter = &ipRateLimiter{
		Limiter: rate.NewLimiter(ipMaxRatePerSec, ipMaxBurstSize),
		lastHit: time.Now(),
	}
	ipHTTPRateLimiter[ip] = limiter
	return limiter
}

// NextID returns a unique ID to identify a request-type message.
func NextID() uint64 {
	return atomic.AddUint64(&idCounter, 1)
}

// MsgHandler describes a handler for a specific message route.
type MsgHandler func(Link, *msgjson.Message) *msgjson.Error

// rpcRoutes maps message routes to the handlers.
var rpcRoutes = make(map[string]MsgHandler)

// HTTPHandler describes a handler for an HTTP route.
type HTTPHandler func(thing interface{}) (interface{}, error)

// httpRoutes maps HTTP routes to the handlers.
var httpRoutes = make(map[string]HTTPHandler)

// Route registers a handler for a specified route. The handler map is global
// and has no mutex protection. All calls to Route should be done before the
// Server is started.
func Route(route string, handler MsgHandler) {
	if route == "" {
		panic("Route: route is empty string")
	}
	_, alreadyHave := rpcRoutes[route]
	if alreadyHave {
		panic(fmt.Sprintf("Route: double registration: %s", route))
	}
	rpcRoutes[route] = handler
}

// RouteHandler gets the handler registered to the specified route, if it
// exists.
func RouteHandler(route string) MsgHandler {
	return rpcRoutes[route]
}

func RegisterHTTP(route string, handler HTTPHandler) {
	if route == "" {
		panic("RegisterHTTP: route is empty string")
	}
	_, alreadyHave := httpRoutes[route]
	if alreadyHave {
		panic(fmt.Sprintf("RegisterHTTP: double registration: %s", route))
	}
	httpRoutes[route] = handler
}

// The RPCConfig is the server configuration settings and the only argument
// to the server's constructor.
type RPCConfig struct {
	// ListenAddrs are the addresses on which the server will listen.
	ListenAddrs []string
	// The location of the TLS keypair files. If they are not already at the
	// specified location, a keypair with a self-signed certificate will be
	// generated and saved to these locations.
	RPCKey  string
	RPCCert string
	// AltDNSNames specifies allowable request addresses for an auto-generated
	// TLS keypair. Changing AltDNSNames does not force the keypair to be
	// regenerated. To regenerate, delete or move the old files.
	AltDNSNames []string
	// DisableDataAPI will disable all traffic to the HTTP data API routes.
	DisableDataAPI bool
}

// allower is satisfied by rate.Limiter.
type allower interface {
	Allow() bool
}

// routeLimiter contains a set of rate limiters for individual routes, and a
// cumulative limiter applied after defined routers are applied. No limiter is
// applied to an unspecified route.
type routeLimiter struct {
	routes     map[string]allower
	cumulative allower // only used for defined routes
}

func (rl *routeLimiter) allow(route string) bool {
	// To apply the cumulative limiter to all routes including those without
	// their own limiter, we would apply it here. Maybe go with this if we are
	// confident it's not going to interfere with init/redeem or others.
	// if !rl.cumulative.Allow() {
	// 	return false
	// }
	limiter := rl.routes[route]
	if limiter == nil {
		return true // free
	}
	return rl.cumulative.Allow() && limiter.Allow()
}

// newRouteLimiter creates a route-based rate limiter. It should be applied to
// all connections from a given IP address.
func newRouteLimiter() *routeLimiter {
	// Some routes share a limiter to aggregate request stats:
	statusLimiter := rate.NewLimiter(wsRateStatus, wsBurstStatus)
	orderLimiter := rate.NewLimiter(wsRateOrder, wsBurstOrder)
	infoLimiter := rate.NewLimiter(wsRateInfo, wsBurstInfo)
	return &routeLimiter{
		cumulative: rate.NewLimiter(wsRateTotal, wsBurstTotal),
		routes: map[string]allower{
			// Meter the 'register' route the most.
			msgjson.RegisterRoute: rate.NewLimiter(wsRateRegister, wsBurstRegister),
			// Status checking of matches and orders
			msgjson.MatchStatusRoute: statusLimiter,
			msgjson.OrderStatusRoute: statusLimiter,
			// Order submission
			msgjson.LimitRoute:  orderLimiter,
			msgjson.MarketRoute: orderLimiter,
			msgjson.CancelRoute: orderLimiter,
			// Order book subscription with full snapshot
			msgjson.OrderBookRoute: rate.NewLimiter(wsRateBook, wsBurstBook),
			// Config and fee rate
			msgjson.FeeRateRoute: infoLimiter,
			msgjson.ConfigRoute:  infoLimiter,
		},
	}
}

// ipWsLimiter facilitates connection counting for a source IP address to
// aggregate requests stats by a single rate limiter.
type ipWsLimiter struct {
	conns   int64
	cleaner *time.Timer // when conns drops to zero, set a cleanup timer
	*routeLimiter
}

// Server is a low-level communications hub. It supports websocket clients
// and an HTTP API.
type Server struct {
	// One listener for each address specified at (RPCConfig).ListenAddrs.
	listeners []net.Listener

	// The client map indexes each wsLink by its id.
	clientMtx sync.RWMutex
	clients   map[uint64]*wsLink
	counter   uint64 // for generating unique client IDs

	// wsLimiters manages per-IP per-route websocket connection request rate
	// limiters that are not subject to server-wide rate limits or affected by
	// disabling of the data API (Server.dataEnabled).
	wsLimiterMtx sync.Mutex // the map and the fields of each limiter
	wsLimiters   map[dex.IPKey]*ipWsLimiter
	v6Prefixes   map[dex.IPKey]int // just debugging presently

	// The quarantine map maps IP addresses to a time in which the quarantine will
	// be lifted.
	banMtx     sync.RWMutex
	quarantine map[dex.IPKey]time.Time

	dataEnabled uint32 // atomic
}

// NewServer constructs a Server that should be started with Run. The server is
// TLS-only, and will generate a key pair with a self-signed certificate if one
// is not provided as part of the RPCConfig. The server also maintains a
// IP-based quarantine to short-circuit to an error response for misbehaving
// clients, if necessary.
func NewServer(cfg *RPCConfig) (*Server, error) {
	// Find or create the key pair.
	keyExists := fileExists(cfg.RPCKey)
	certExists := fileExists(cfg.RPCCert)
	if certExists == !keyExists {
		return nil, fmt.Errorf("missing cert pair file")
	}
	if !keyExists && !certExists {
		err := genCertPair(cfg.RPCCert, cfg.RPCKey, cfg.AltDNSNames)
		if err != nil {
			return nil, err
		}
	}
	keypair, err := tls.LoadX509KeyPair(cfg.RPCCert, cfg.RPCKey)
	if err != nil {
		return nil, err
	}

	// Prepare the TLS configuration.
	tlsConfig := tls.Config{
		Certificates: []tls.Certificate{keypair},
		MinVersion:   tls.VersionTLS12,
	}
	// Parse the specified listen addresses and create the []net.Listener.
	ipv4ListenAddrs, ipv6ListenAddrs, _, err := parseListeners(cfg.ListenAddrs)
	if err != nil {
		return nil, err
	}
	listeners := make([]net.Listener, 0, len(ipv6ListenAddrs)+len(ipv4ListenAddrs))
	for _, addr := range ipv4ListenAddrs {
		listener, err := tls.Listen("tcp4", addr, &tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("Can't listen on %s: %w", addr, err)
		}
		listeners = append(listeners, listener)
	}
	for _, addr := range ipv6ListenAddrs {
		listener, err := tls.Listen("tcp6", addr, &tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("Can't listen on %s: %w", addr, err)
		}
		listeners = append(listeners, listener)
	}
	if len(listeners) == 0 {
		return nil, fmt.Errorf("RPCS: No valid listen address")
	}
	var dataEnabled uint32 = 1
	if cfg.DisableDataAPI {
		dataEnabled = 0
	}

	return &Server{
		listeners:   listeners,
		clients:     make(map[uint64]*wsLink),
		wsLimiters:  make(map[dex.IPKey]*ipWsLimiter),
		v6Prefixes:  make(map[dex.IPKey]int),
		quarantine:  make(map[dex.IPKey]time.Time),
		dataEnabled: dataEnabled,
	}, nil
}

// Run starts the server. Run should be called only after all routes are
// registered.
func (s *Server) Run(ctx context.Context) {
	log.Trace("Starting RPC server")

	// Create an HTTP router, putting a couple of useful middlewares in place.
	mux := chi.NewRouter()
	mux.Use(middleware.RealIP)
	mux.Use(middleware.Recoverer)
	httpServer := &http.Server{
		Handler:      mux,
		ReadTimeout:  rpcTimeoutSeconds * time.Second, // slow requests should not hold connections opened
		WriteTimeout: rpcTimeoutSeconds * time.Second, // hung responses must die
		//BaseContext:  func(net.Listener) context.Context { return ctx },
	}

	var wg sync.WaitGroup

	// Websocket endpoint.
	mux.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		ip := dex.NewIPKey(r.RemoteAddr)

		if s.isQuarantined(ip) {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		if s.clientCount() >= rpcMaxClients {
			http.Error(w, "server at maximum capacity", http.StatusServiceUnavailable)
			return
		}
		// Check websocket connection count for this IP before upgrading the
		// conn so we can send an HTTP error code, but check again after
		// upgrade/hijack so they cannot initiate many simultaneously.
		if s.ipConnCount(ip) >= rpcMaxConnsPerIP {
			http.Error(w, "too many connections from your address", http.StatusServiceUnavailable)
			return
		}
		wsConn, err := ws.NewConnection(w, r, pongWait)
		if err != nil {
			log.Errorf("ws connection error: %v", err)
			return
		}

		// http.Server.Shutdown waits for connections to complete (such as this
		// http.HandlerFunc), but not the long running upgraded websocket
		// connections. We must wait on each websocketHandler to return in
		// response to disconnectClients.
		log.Debugf("Starting websocket handler for %s", r.RemoteAddr) // includes source port
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.websocketHandler(ctx, wsConn, ip)
		}()
	})

	// Data API endpoints.
	mux.Route("/api", func(rr chi.Router) {
		if log.Level() == dex.LevelTrace {
			rr.Use(middleware.Logger)
		}
		rr.Use(s.limitRate)
		rr.Get("/config", routeHandler(msgjson.ConfigRoute))
		rr.Get("/spots", routeHandler(msgjson.SpotsRoute))
		rr.With(candleParamsParser).Get("/candles/{baseSymbol}/{quoteSymbol}/{binSize}", routeHandler(msgjson.CandlesRoute))
		rr.With(candleParamsParser).Get("/candles/{baseSymbol}/{quoteSymbol}/{binSize}/{count}", routeHandler(msgjson.CandlesRoute))
		rr.With(orderBookParamsParser).Get("/orderbook/{baseSymbol}/{quoteSymbol}", routeHandler(msgjson.OrderBookRoute))
	})

	// Start serving.
	for _, listener := range s.listeners {
		wg.Add(1)
		go func(listener net.Listener) {
			log.Infof("RPC server listening on %s", listener.Addr())
			err := httpServer.Serve(listener)
			if !errors.Is(err, http.ErrServerClosed) {
				log.Warnf("unexpected (http.Server).Serve error: %v", err)
			}
			log.Debugf("RPC listener done for %s", listener.Addr())
			wg.Done()
		}(listener)
	}

	// Run a periodic routine to keep the ipHTTPRateLimiter map clean.
	go func() {
		ticker := time.NewTicker(time.Minute * 5)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rateLimiterMtx.Lock()
				for ip, limiter := range ipHTTPRateLimiter {
					if time.Since(limiter.lastHit) > time.Minute {
						delete(ipHTTPRateLimiter, ip)
					}
				}
				rateLimiterMtx.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()

	<-ctx.Done()

	// Shutdown the server. This stops all listeners and waits for connections.
	log.Infof("RPC server shutting down...")
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := httpServer.Shutdown(ctxTimeout)
	if err != nil {
		log.Warnf("http.Server.Shutdown: %v", err)
	}

	// Stop and disconnect websocket clients.
	s.disconnectClients()

	// When the http.Server is shut down, all websocket clients are gone, and
	// the listener goroutines have returned, the server is shut down.
	wg.Wait()
	log.Infof("RPC server shutdown complete")
}

// Check if the IP address is quarantined.
func (s *Server) isQuarantined(ip dex.IPKey) bool {
	s.banMtx.RLock()
	banTime, banned := s.quarantine[ip]
	s.banMtx.RUnlock()
	if banned {
		// See if the ban has expired.
		if time.Now().After(banTime) {
			s.banMtx.Lock()
			delete(s.quarantine, ip)
			s.banMtx.Unlock()
			banned = false
		}
	}
	return banned
}

// Quarantine the specified IP address.
func (s *Server) banish(ip dex.IPKey) {
	s.banMtx.Lock()
	defer s.banMtx.Unlock()
	s.quarantine[ip] = time.Now().Add(banishTime)
}

// wsLimiter gets any existing routeLimiter for an IP incrementing the
// connection count for the address, or creates a new one. The caller should use
// wsLimiterDone after the connection that uses routeLimiter is closed. Loopback
// addresses always get a new unshared limiter. NewIPKey should be used to
// create an IPKey with interface bits masked out. This is not perfect with
// respect to remote IPv6 hosts assigned multiple subnets (up to 16 bits worth).
// Disable IPv6 if this is not acceptable.
func (s *Server) wsLimiter(ip dex.IPKey) *routeLimiter {
	// If the ip is a loopback address, this likely indicates a hidden service
	// or misconfigured reverse proxy, and it is undesirable for many such
	// connections to share a common limiter. To avoid this, return a new
	// untracked limiter for such clients.
	if ip.IsLoopback() {
		return newRouteLimiter()
	}

	s.wsLimiterMtx.Lock()
	defer s.wsLimiterMtx.Unlock()
	prefix := ip.PrefixV6()
	if prefix != nil { // not ipv4
		if n := s.v6Prefixes[*prefix]; n > 0 {
			log.Infof("Detected %d active IPv6 connections with same prefix %v", n, prefix)
			// ip = *prefix // Use a prefix-aggregated limiter.
		}
	}

	if l := s.wsLimiters[ip]; l != nil {
		if l.conns >= rpcMaxConnsPerIP {
			return nil
		}
		l.conns++
		if prefix != nil {
			s.v6Prefixes[*prefix]++
		}
		if l.cleaner != nil { // l.conns was zero
			log.Debugf("Restoring active rate limiter for %v", ip)
			// Even if the timer already fired, we won the race to the lock and
			// incremented conns so the cleaner func will be a no-op.
			l.cleaner.Stop() // false means timer fired already
			l.cleaner = nil
		}
		return l.routeLimiter
	}

	limiter := newRouteLimiter()
	s.wsLimiters[ip] = &ipWsLimiter{
		conns:        1,
		routeLimiter: limiter,
	}
	if prefix != nil {
		s.v6Prefixes[*prefix]++
	}
	return limiter
}

// wsLimiterDone decrements the connection count for the IP address'
// routeLimiter, and deletes it entirely if there are no remaining connections
// from this address.
func (s *Server) wsLimiterDone(ip dex.IPKey) {
	s.wsLimiterMtx.Lock()
	defer s.wsLimiterMtx.Unlock()

	if prefix := ip.PrefixV6(); prefix != nil {
		switch s.v6Prefixes[*prefix] {
		case 0:
		case 1:
			delete(s.v6Prefixes, *prefix)
		default:
			s.v6Prefixes[*prefix]--
		}
	}

	wsLimiter := s.wsLimiters[ip]
	if wsLimiter == nil {
		return // untracked limiter
		/*
			// Try a prefix-aggregated limiter.
			if prefix == nil {
				return
			}
			wsLimiter = s.wsLimiters[*prefix]
			if wsLimiter == nil {
				return
			}
			ip = *prefix
		*/
	}

	wsLimiter.conns--
	if wsLimiter.conns < 1 {
		// Start a cleanup timer.
		wsLimiter.cleaner = time.AfterFunc(time.Minute, func() {
			s.wsLimiterMtx.Lock()
			defer s.wsLimiterMtx.Unlock()
			if wsLimiter.conns < 1 {
				log.Debugf("Forgetting rate limiter for %v", ip)
				delete(s.wsLimiters, ip)
			} // else lost the race to the mutex, don't remove
		})
	}
}

// websocketHandler handles a new websocket client by creating a new wsClient,
// starting it, and blocking until the connection closes. This method should be
// run as a goroutine.
func (s *Server) websocketHandler(ctx context.Context, conn ws.Connection, ip dex.IPKey) {
	addr := ip.String()
	log.Tracef("New websocket client %s", addr)

	// Create a new websocket client to handle the new websocket connection
	// and wait for it to shutdown.  Once it has shutdown (and hence
	// disconnected), remove it.
	dataRoutesMeter := func() (int, error) { return s.meterIP(ip) } // includes global limiter and may be disabled
	wsLimiter := s.wsLimiter(ip)
	if wsLimiter == nil { // too many active ws conns from this IP
		log.Warnf("Too many websocket connections from %v", ip)
		return
	}
	defer s.wsLimiterDone(ip)
	client := newWSLink(addr, conn, wsLimiter, dataRoutesMeter)
	cm, err := s.addClient(ctx, client)
	if err != nil {
		log.Errorf("Failed to add client %s", addr)
		return
	}
	defer s.removeClient(client.id)

	// The connection remains until the connection is lost or the link's
	// disconnect method is called (e.g. via disconnectClients).
	cm.Wait()

	// If the ban flag is set, quarantine the client's IP address.
	if client.ban {
		s.banish(ip)
	}
	log.Tracef("Disconnected websocket client %s", addr)
}

// Broadcast sends a message to all connected clients. The message should be a
// notification. See msgjson.NewNotification.
func (s *Server) Broadcast(msg *msgjson.Message) {
	s.clientMtx.RLock()
	defer s.clientMtx.RUnlock()

	log.Infof("Broadcasting %s for route %s to %d clients...", msg.Type, msg.Route, len(s.clients))
	if log.Level() <= dex.LevelTrace { // don't marshal unless needed
		log.Tracef("Broadcast: %q", msg.String())
	}

	for id, cl := range s.clients {
		if err := cl.Send(msg); err != nil {
			log.Debugf("Send to client %d at %s failed: %v", id, cl.Addr(), err)
			cl.Disconnect() // triggers return of websocketHandler, and removeClient
		}
	}
}

// EnableDataAPI enables or disables the HTTP data API endpoints.
func (s *Server) EnableDataAPI(yes bool) {
	if yes {
		atomic.StoreUint32(&s.dataEnabled, 1)
	} else {
		atomic.StoreUint32(&s.dataEnabled, 0)
	}
}

// disconnectClients calls disconnect on each wsLink, but does not remove it
// from the Server's client map.
func (s *Server) disconnectClients() {
	s.clientMtx.Lock()
	for _, link := range s.clients {
		link.Disconnect()
	}
	s.clientMtx.Unlock()
}

// addClient assigns the client an ID, adds it to the map, and attempts to
// connect.
func (s *Server) addClient(ctx context.Context, client *wsLink) (*dex.ConnectionMaster, error) {
	s.clientMtx.Lock()
	defer s.clientMtx.Unlock()
	client.id = s.counter
	s.counter++
	s.clients[client.id] = client
	cm := dex.NewConnectionMaster(client)
	return cm, cm.Connect(ctx)
}

// Remove the client from the map.
func (s *Server) removeClient(id uint64) {
	s.clientMtx.Lock()
	delete(s.clients, id)
	s.clientMtx.Unlock()
}

// Get the number of active clients.
func (s *Server) clientCount() uint64 {
	s.clientMtx.RLock()
	defer s.clientMtx.RUnlock()
	return uint64(len(s.clients))
}

// Get the number of websocket connections for a given IP, excluding loopback.
func (s *Server) ipConnCount(ip dex.IPKey) int64 {
	s.wsLimiterMtx.Lock()
	defer s.wsLimiterMtx.Unlock()
	wsl := s.wsLimiters[ip]
	if wsl == nil {
		return 0
	}
	return wsl.conns
}

// filesExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	_, err := os.Stat(name)
	return !os.IsNotExist(err)
}

// genCertPair generates a key/cert pair to the paths provided.
func genCertPair(certFile, keyFile string, altDNSNames []string) error {
	log.Infof("Generating TLS certificates...")

	org := "dcrdex autogenerated cert"
	validUntil := time.Now().Add(10 * 365 * 24 * time.Hour)
	cert, key, err := certgen.NewTLSCertPair(elliptic.P521(), org,
		validUntil, altDNSNames)
	if err != nil {
		return err
	}

	// Write cert and key files.
	if err = os.WriteFile(certFile, cert, 0644); err != nil {
		return err
	}
	if err = os.WriteFile(keyFile, key, 0600); err != nil {
		os.Remove(certFile)
		return err
	}

	log.Infof("Done generating TLS certificates")
	return nil
}

// parseListeners splits the list of listen addresses passed in addrs into
// IPv4 and IPv6 slices and returns them.  This allows easy creation of the
// listeners on the correct interface "tcp4" and "tcp6".  It also properly
// detects addresses which apply to "all interfaces" and adds the address to
// both slices.
func parseListeners(addrs []string) ([]string, []string, bool, error) {
	ipv4ListenAddrs := make([]string, 0, len(addrs))
	ipv6ListenAddrs := make([]string, 0, len(addrs))
	haveWildcard := false

	for _, addr := range addrs {
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			// Shouldn't happen due to already being normalized.
			return nil, nil, false, err
		}

		// Empty host is both IPv4 and IPv6.
		if host == "" {
			ipv4ListenAddrs = append(ipv4ListenAddrs, addr)
			ipv6ListenAddrs = append(ipv6ListenAddrs, addr)
			haveWildcard = true
			continue
		}

		// Strip IPv6 zone id if present since net.ParseIP does not
		// handle it.
		zoneIndex := strings.LastIndex(host, "%")
		if zoneIndex > 0 {
			host = host[:zoneIndex]
		}

		// Parse the IP.
		ip := net.ParseIP(host)
		if ip == nil {
			return nil, nil, false, fmt.Errorf("'%s' is not a valid IP address", host)
		}

		// To4 returns nil when the IP is not an IPv4 address, so use
		// this determine the address type.
		if ip.To4() == nil {
			ipv6ListenAddrs = append(ipv6ListenAddrs, addr)
		} else {
			ipv4ListenAddrs = append(ipv4ListenAddrs, addr)
		}
	}
	return ipv4ListenAddrs, ipv6ListenAddrs, haveWildcard, nil
}

// routeHandler creates a HandlerFunc for a route. Middleware should have
// already processed the request and added the request struct to the Context.
func routeHandler(route string) func(w http.ResponseWriter, r *http.Request) {
	handler := httpRoutes[route]
	if handler == nil {
		panic("no known handler")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := handler(r.Context().Value(ctxThing))
		if err != nil {
			writeJSONWithStatus(w, map[string]string{"error": err.Error()}, http.StatusBadRequest)
			return
		}
		writeJSONWithStatus(w, resp, http.StatusOK)
	}
}

// writeJSONWithStatus writes the JSON response with the specified HTTP response
// code.
func writeJSONWithStatus(w http.ResponseWriter, thing interface{}, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b, err := json.Marshal(thing)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Errorf("JSON encode error: %v", err)
		return
	}
	w.WriteHeader(code)
	_, err = w.Write(append(b, byte('\n')))
	if err != nil {
		log.Errorf("Write error: %v", err)
	}
}
