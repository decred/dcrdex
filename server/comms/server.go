// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package comms

import (
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

	"decred.org/dcrdex/dex/msgjson"
	"github.com/decred/dcrd/certgen"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/gorilla/websocket"
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
	// websocket.Upgrader is the preferred method of upgrading a request to a
	// websocket connection.
	upgrader = websocket.Upgrader{}

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

// rpcRoute describes a handler for a specific message route.
type rpcRoute func(Link, *msgjson.Message) *msgjson.Error

// rpcRoutes maps message routes to the handlers.
var rpcRoutes = make(map[string]rpcRoute)

// Route registers a handler for a specified route. The handler map is global
// and has no mutex protection. All calls to Route should be done before the
// Server is started.
func Route(route string, handler rpcRoute) {
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
func RouteHandler(route string) rpcRoute {
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
	// A WaitGroup for shutdown synchronization.
	wg sync.WaitGroup
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

// Start starts the server. Start should be called only after all routes are
// registered.
func (s *Server) Start() {
	log.Trace("Starting RPC server")

	// Create an HTTP router, putting a couple of useful middlewares in place.
	mux := chi.NewRouter()
	mux.Use(middleware.RealIP)
	mux.Use(middleware.Recoverer)
	httpServer := &http.Server{
		Handler:      mux,
		ReadTimeout:  rpcTimeoutSeconds * time.Second, // slow requests should not hold connections opened
		WriteTimeout: rpcTimeoutSeconds * time.Second, // hung responses must die
	}

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
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			var hsErr websocket.HandshakeError
			if errors.As(err, &hsErr) {
				log.Errorf("Unexpected websocket error: %v",
					err)
			}
			http.Error(w, "400 Bad Request.", http.StatusBadRequest)
			return
		}
		pongHandler := func(string) error {
			return ws.SetReadDeadline(time.Now().Add(pingPeriod + pongWait))
		}
		err = pongHandler("")
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		ws.SetPongHandler(pongHandler)
		go s.websocketHandler(ws, ip)
	})

	// Start serving.
	for _, listener := range s.listeners {
		s.wg.Add(1)
		go func(listener net.Listener) {
			log.Infof("RPC server listening on %s", listener.Addr())
			err := httpServer.Serve(listener)
			if !errors.Is(err, http.ErrServerClosed) {
				log.Warnf("unexpected (http.Server).Serve error: %v", err)
			}
			log.Tracef("RPC listener done for %s", listener.Addr())
			s.wg.Done()
		}(listener)
	}
}

// Stop closes all of the listeners and waits for the WaitGroup.
func (s *Server) Stop() {
	log.Warnf("RPC server shutting down")
	for _, listener := range s.listeners {
		err := listener.Close()
		if err != nil {
			log.Errorf("Problem shutting down rpc: %v", err)
		}
	}
	s.wg.Wait()
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
func (s *Server) websocketHandler(conn wsConnection, ip string) {
	log.Tracef("New websocket client %s", ip)

	// Create a new websocket client to handle the new websocket connection
	// and wait for it to shutdown.  Once it has shutdown (and hence
	// disconnected), remove it.
	client := newWSLink(ip, conn)
	s.addClient(client)
	client.start()
	client.waitForShutdown()
	s.removeClient(client.id)
	// If the ban flag is set, quarantine the client's IP address.
	if client.ban {
		s.banish(client.ip)
	}
	log.Tracef("Disconnected websocket client %s", ip)
}

// addClient assigns the client an ID and adds it to the map.
func (s *Server) addClient(client *wsLink) {
	s.clientMtx.Lock()
	defer s.clientMtx.Unlock()
	client.id = s.counter
	s.counter++
	s.clients[client.id] = client
}

// Remove the client from the map.
func (s *Server) removeClient(id uint64) {
	s.clientMtx.Lock()
	defer s.clientMtx.Unlock()
	delete(s.clients, id)
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
