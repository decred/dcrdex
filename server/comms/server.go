// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package comms

import (
	"context"
	"crypto/elliptic"
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
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
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
)

const (
	// rpcTimeoutSeconds is the number of seconds a connection to the
	// RPC server is allowed to stay open without authenticating before it
	// is closed.
	rpcTimeoutSeconds = 10

	// rpcMaxClients is the maximum number of active websocket connections
	// allowed.
	rpcMaxClients = 10000

	// banishTime is the default duration of a client quarantine.
	banishTime = time.Hour
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

var idCounter uint64

// NextID returns a unique ID to identify a request-type message.
func NextID() uint64 {
	return atomic.AddUint64(&idCounter, 1)
}

// MsgHandler describes a handler for a specific message route.
type MsgHandler func(Link, *msgjson.Message) *msgjson.Error

// rpcRoutes maps message routes to the handlers.
var rpcRoutes = make(map[string]MsgHandler)

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
}

// Server is a low-level communications hub. It supports websocket clients
// and an HTTP API.
type Server struct {
	// One listener for each address specified at (RPCConfig).ListenAddrs.
	listeners []net.Listener
	// Protect the client map, which maps the (link).id to the client itself.
	clientMtx sync.RWMutex
	clients   map[uint64]*wsLink
	// A simple counter for generating unique client IDs. The counter is also
	// protected by the clientMtx.
	counter uint64
	// The quarantine map maps IP addresses to a time in which the quarantine will
	// be lifted.
	banMtx     sync.RWMutex
	quarantine map[string]time.Time
}

// A constructor for an Server. The Server handles a map of clients, each
// with at least 3 goroutines for communications. The server is TLS-only, and
// will generate a key pair with a self-signed certificate if one is not
// provided as part of the RPCConfig. The server also maintains a IP-based
// quarantine to short-circuit to an error response for misbehaving clients, if
// necessary.
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
			return nil, fmt.Errorf("Can't listen on %s: %v", addr, err)
		}
		listeners = append(listeners, listener)
	}
	for _, addr := range ipv6ListenAddrs {
		listener, err := tls.Listen("tcp6", addr, &tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("Can't listen on %s: %v", addr, err)
		}
		listeners = append(listeners, listener)
	}
	if len(listeners) == 0 {
		return nil, fmt.Errorf("RPCS: No valid listen address")
	}

	return &Server{
		listeners:  listeners,
		clients:    make(map[uint64]*wsLink),
		quarantine: make(map[string]time.Time),
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
		// If the IP address includes a port, remove it.
		ip := r.RemoteAddr
		// If a host:port can be parsed, the IP is only the host portion.
		host, _, err := net.SplitHostPort(ip)
		if err == nil && host != "" {
			ip = host
		}

		if s.isQuarantined(ip) {
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		if s.clientCount() >= rpcMaxClients {
			http.Error(w, "server at maximum capacity", http.StatusServiceUnavailable)
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
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.websocketHandler(ctx, wsConn, ip)
		}()
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
func (s *Server) isQuarantined(ip string) bool {
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
func (s *Server) banish(ip string) {
	s.banMtx.Lock()
	defer s.banMtx.Unlock()
	s.quarantine[ip] = time.Now().Add(banishTime)
}

// websocketHandler handles a new websocket client by creating a new wsClient,
// starting it, and blocking until the connection closes. This method should be
// run as a goroutine.
func (s *Server) websocketHandler(ctx context.Context, conn ws.Connection, ip string) {
	log.Debugf("New websocket client %s", ip)

	// Create a new websocket client to handle the new websocket connection
	// and wait for it to shutdown.  Once it has shutdown (and hence
	// disconnected), remove it.
	client := newWSLink(ip, conn)
	cm, err := s.addClient(ctx, client)
	if err != nil {
		log.Errorf("Failed to add client %s", ip)
		return
	}
	defer s.removeClient(client.id)

	// The connection remains until the connection is lost or the link's
	// disconnect method is called (e.g. via disconnectClients).
	cm.Wait()

	// If the ban flag is set, quarantine the client's IP address.
	if client.ban {
		s.banish(client.IP())
	}
	log.Tracef("Disconnected websocket client %s", ip)
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
			log.Debugf("Send to client %d at %s failed: %v", id, cl.IP(), err)
			cl.Disconnect() // triggers return of websocketHandler, and removeClient
		}
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
	if err = ioutil.WriteFile(certFile, cert, 0644); err != nil {
		return err
	}
	if err = ioutil.WriteFile(keyFile, key, 0600); err != nil {
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
