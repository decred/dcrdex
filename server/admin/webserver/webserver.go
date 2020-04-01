// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"golang.org/x/crypto/ssh/terminal"

	dexsrv "decred.org/dcrdex/server/dex"
	"github.com/decred/slog"
)

const (
	// rpcTimeoutSeconds is the number of seconds a connection to the
	// web server is allowed to stay open without authenticating before it
	// is closed.
	rpcTimeoutSeconds = 10
)

var (
	// Check that *dexsrv.DEX satifies ServerCore.
	_   ServerCore = (*dexsrv.DEX)(nil)
	log slog.Logger
)

// ServerCore is satisfied by core.Core.
type ServerCore interface{}

// WebServer is a multi-client https server.
type WebServer struct {
	core      ServerCore
	addr      string
	tlsConfig *tls.Config
	srv       *http.Server
	authSHA   [32]byte
}

// Config holds variables needed to create a new web Server.
type Config struct {
	Core            ServerCore
	Addr, Cert, Key string
	AuthSHA         [32]byte
}

// UseLogger sets the logger for the webserver package.
func UseLogger(logger slog.Logger) {
	log = logger
}

// PasswordPrompt prompts the user to enter a password for use with the admin
// webserver and returns its sha256 hash. Password must not be an empty string.
func PasswordPrompt() ([32]byte, error) {
	var authSHA [32]byte
	fmt.Println("Enter admin webserver password:")
	password, err := terminal.ReadPassword(syscall.Stdin)
	if err != nil {
		return authSHA, err
	}
	if password == nil {
		return authSHA, errors.New("admin webserver password must be set to use the admin webserver")
	}
	authSHA = sha256.Sum256(password)
	// Zero password bytes.
	for i := range password {
		password[i] = 0x00
	}
	return authSHA, nil
}

// filesExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	_, err := os.Stat(name)
	return !os.IsNotExist(err)
}

// New is the constructor for a new WebServer.
func New(cfg *Config) (*WebServer, error) {
	// Find the key pair.
	if !fileExists(cfg.Key) || !fileExists(cfg.Cert) {
		return nil, fmt.Errorf("missing certificates")
	}

	keypair, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
	if err != nil {
		return nil, err
	}

	// Prepare the TLS configuration.
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{keypair},
		MinVersion:   tls.VersionTLS12,
	}

	// Create an HTTP router.
	mux := chi.NewRouter()
	httpServer := &http.Server{
		Handler:      mux,
		ReadTimeout:  rpcTimeoutSeconds * time.Second, // slow requests should not hold connections opened
		WriteTimeout: rpcTimeoutSeconds * time.Second, // hung responses must die
	}

	// Make the server.
	s := &WebServer{
		core:      cfg.Core,
		srv:       httpServer,
		addr:      cfg.Addr,
		tlsConfig: tlsConfig,
		authSHA:   cfg.AuthSHA,
	}

	// Middleware
	mux.Use(middleware.Recoverer)
	mux.Use(middleware.RealIP)
	mux.Use(s.authMiddleware)

	// api endpoints
	mux.Route("/api", func(r chi.Router) {
		r.Use(middleware.AllowContentType("application/json"))
		r.Get("/ping", s.apiPing)
	})

	return s, nil
}

// Run starts the web server.
func (s *WebServer) Run(ctx context.Context) {
	// Create listener.
	listener, err := tls.Listen("tcp", s.addr, s.tlsConfig)
	if err != nil {
		log.Errorf("can't listen on %s. admin webserver quitting: %v", s.addr, err)
		return
	}

	// Close the listener on context cancellation.
	shutdown := make(chan struct{})
	go func() {
		defer close(shutdown)
		<-ctx.Done()

		if err := s.srv.Shutdown(context.Background()); err != nil {
			// Error from closing listeners:
			log.Errorf("HTTP server Shutdown: %v", err)
		}
	}()
	log.Infof("admin webserver listening on %s", s.addr)
	if err := s.srv.Serve(listener); err != http.ErrServerClosed {
		log.Warnf("unexpected (http.Server).Serve error: %v", err)
	}

	// Wait for Shutdown.
	<-shutdown
	log.Infof("admin webserver off")
}

// authMiddleware checks incoming requests for authentication.
func (s *WebServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || s.authSHA != sha256.Sum256([]byte(pass)) {
			log.Warnf("authentication failure from ip: %s for user: %s with password: %s", r.RemoteAddr, user, pass)
			w.Header().Add("WWW-Authenticate", `Basic realm="dex admin"`)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}
		log.Debugf("authenticated user %s with ip: %s", user, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}
