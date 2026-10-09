// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/ws"
)

// meshWSPath is the HTTP path the mesh websocket endpoint is served on.
const meshWSPath = "/meshws"

// meshServerConfig is the listener configuration.
type meshServerConfig struct {
	ListenAddr string
	RPCKey     string
	RPCCert    string
	NoTLS      bool
}

// meshServer is the HTTP server that listens for inbound mesh connections.
type meshServer struct {
	log      dex.Logger
	routes   map[string]meshRoute
	listener net.Listener

	mtx    sync.Mutex
	closed bool
}

// newMeshServer binds the listen address and prepares the server. The port is
// bound here, so a bind failure is reported before Run.
func newMeshServer(cfg *meshServerConfig, routes map[string]meshRoute, log dex.Logger) (*meshServer, error) {
	if cfg == nil {
		return nil, errors.New("nil mesh server config")
	}
	if cfg.ListenAddr == "" {
		return nil, errors.New("empty mesh listen address")
	}

	tlsConfig, err := buildServerTLSConfig(cfg)
	if err != nil {
		return nil, err
	}

	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return nil, err
	}
	if tlsConfig != nil {
		listener = tls.NewListener(listener, tlsConfig)
	}

	return &meshServer{
		log:      log,
		routes:   routes,
		listener: listener,
	}, nil
}

// buildServerTLSConfig returns the tls.Config for the mesh listener, or nil
// if NoTLS is set.
func buildServerTLSConfig(cfg *meshServerConfig) (*tls.Config, error) {
	if cfg.NoTLS {
		return nil, nil
	}
	if cfg.RPCKey == "" || cfg.RPCCert == "" {
		return nil, errors.New("TLS enabled but RPCKey or RPCCert is empty")
	}
	if !dex.FileExists(cfg.RPCKey) || !dex.FileExists(cfg.RPCCert) {
		return nil, errors.New("missing cert pair file")
	}
	keypair, err := tls.LoadX509KeyPair(cfg.RPCCert, cfg.RPCKey)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{keypair},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// acceptWS upgrades the HTTP request to a websocket and wraps it in an
// rpcConn.
func (s *meshServer) acceptWS(ctx context.Context, w http.ResponseWriter, r *http.Request) (*rpcConn, *sync.WaitGroup, error) {
	wsConn, err := ws.NewConnection(w, r, meshPingPeriod*2)
	if err != nil {
		return nil, nil, err
	}

	conn := newRPCConn(ctx, r.RemoteAddr, wsConn, s.routes, s.log)
	wg, err := conn.connect()
	if err != nil {
		return nil, nil, err
	}

	return conn, wg, nil
}

// Run serves the listener until ctx ends or the server fails.
func (s *meshServer) Run(ctx context.Context) error {
	connCtx, cancelConns := context.WithCancel(ctx)
	defer cancelConns()

	var wg sync.WaitGroup
	mux := http.NewServeMux()
	mux.HandleFunc(meshWSPath, s.handleMeshWS(connCtx, &wg))

	httpServer := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	serveErr := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.log.Infof("Mesh server listening on %s", s.listener.Addr())
		if err := httpServer.Serve(s.listener); !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	var runErr error
	select {
	case <-ctx.Done():
	case runErr = <-serveErr:
	}

	cancelConns()
	s.mtx.Lock()
	s.closed = true
	s.mtx.Unlock()
	s.shutdown(httpServer)
	wg.Wait()

	return runErr
}

// handleMeshWS returns the HTTP handler for the websocket path. The handler
// accepts the connection and adds a goroutine to wg that waits for the
// connection to end.
func (s *meshServer) handleMeshWS(ctx context.Context, wg *sync.WaitGroup) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, connWG, err := s.acceptWS(ctx, w, r)
		if err != nil {
			if errors.Is(err, ws.ErrHandshake) {
				s.log.Debug(err)
			} else {
				s.log.Errorf("mesh websocket connection error: %v", err)
			}
			return
		}

		s.mtx.Lock()
		if s.closed {
			s.mtx.Unlock()
			return
		}

		wg.Add(1)
		s.mtx.Unlock()
		go func() {
			defer wg.Done()
			<-conn.Done()
			connWG.Wait()
		}()
	}
}

func (s *meshServer) shutdown(httpServer *http.Server) {
	ctxTimeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctxTimeout); err != nil {
		s.log.Warnf("mesh http.Server.Shutdown: %v", err)
	}
}
