// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/core"
	"github.com/decred/slog"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
)

const (
	// rpcTimeoutSeconds is the number of seconds a connection to the
	// RPC server is allowed to stay open without authenticating before it
	// is closed.
	rpcTimeoutSeconds = 10
	// darkModeCK is the cookie key for dark mode.
	darkModeCK = "darkMode"
	// authCK is the authorization token cookie key.
	authCK = "dexauth"
	// authCV is the authorization middleware contextual value key.
	authCV = "authctx"
)

var log slog.Logger

// clientCore is satisfied by core.Core.
type clientCore interface {
	ListMarkets() []*core.MarketInfo
	Register(*core.Registration) error
	Login(dex, pw string) error
	Sync(dex string, base, quote uint32) (*core.OrderBook, chan *core.BookUpdate, error)
	Unsync(dex string, base, quote uint32)
	Balance(uint32) (uint64, error)
}

// marketSyncer is used internally to synchronize market subscriptions.
type marketSyncer struct {
	dex   string
	base  uint32
	quote uint32
	kill  func()
}

// WebServer is a single-client http and websocket server enabling a browser
// interface to the DEX client.
type WebServer struct {
	ctx       context.Context
	core      clientCore
	listener  net.Listener
	srv       *http.Server
	html      *templates
	indent    bool
	mtx       sync.RWMutex
	authToken string
	watchers  map[string]int
	clients   map[int32]*wsClient
}

// New is the constructor for a new WebServer.
func New(core clientCore, addr string, logger slog.Logger, reloadHTML bool) (*WebServer, error) {
	log = logger

	folderExists := func(fp string) bool {
		stat, err := os.Stat(fp)
		return err == nil && stat.IsDir()
	}
	fp := filepath.Join

	// Right now, it is expected that the working directory
	// is either the dcrdex root directory, or the WebServer directory itself.
	root := "client/webserver/site"
	if !folderExists(root) {
		root = "site"
		if !folderExists(root) {
			return nil, fmt.Errorf("no HTML template files found")
		}
	}

	// Prepare the templates.
	bb := "bodybuilder"
	tmpl := newTemplates(fp(root, "src/html"), reloadHTML).
		addTemplate("login", bb).
		addTemplate("register", bb).
		addTemplate("markets", bb).
		addTemplate("wallets", bb).
		addTemplate("settings", bb)
	err := tmpl.buildErr()
	if err != nil {
		return nil, err
	}

	// Create an HTTP router.
	mux := chi.NewRouter()
	httpServer := &http.Server{
		Handler:      mux,
		ReadTimeout:  rpcTimeoutSeconds * time.Second, // slow requests should not hold connections opened
		WriteTimeout: rpcTimeoutSeconds * time.Second, // hung responses must die
	}

	// Start serving.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("Can't listen on %s. web server quitting: %v", addr, err)
	}

	// Make the server here so its methods can be registered.
	s := &WebServer{
		core:     core,
		listener: listener,
		srv:      httpServer,
		html:     tmpl,
		watchers: make(map[string]int),
		clients:  make(map[int32]*wsClient),
	}

	// Middleware
	mux.Use(middleware.Recoverer)
	mux.Use(s.authMiddleware)
	// Websocket endpoint
	mux.Get("/ws", s.handleWS)
	// Webpages
	mux.Get("/", s.handleHome)
	mux.Get("/register", s.handleRegister)
	mux.Get("/login", s.handleLogin)
	mux.Get("/markets", s.handleMarkets)
	mux.Get("/wallets", s.handleWallets)
	mux.Get("/settings", s.handleSettings)
	mux.Route("/api", func(r chi.Router) {
		r.Use(middleware.AllowContentType("application/json"))
		r.Post("/register", s.apiRegister)
		r.Post("/login", s.apiLogin)
	})
	// Files
	fileServer(mux, "/js", fp(root, "dist"))
	fileServer(mux, "/css", fp(root, "dist"))
	fileServer(mux, "/img", fp(root, "src/img"))
	fileServer(mux, "/font", fp(root, "src/font"))

	return s, nil
}

// Run starts the web server. Satisfies the runner.Runner interface.
func (s *WebServer) Run(ctx context.Context) {
	s.ctx = ctx
	// Close the listener on context cancellation.
	go func() {
		<-ctx.Done()
		err := s.listener.Close()
		if err != nil {
			log.Errorf("Problem shutting down rpc: %v", err)
		}
		s.mtx.Lock()
		defer s.mtx.Unlock()
		for _, cl := range s.clients {
			cl.Disconnect()
		}
	}()
	log.Infof("Web server listening on %s", s.listener.Addr())
	err := s.srv.Serve(s.listener)
	if err != http.ErrServerClosed {
		log.Warnf("unexpected (http.Server).Serve error: %v", err)
	}
	log.Infof("Web server off")
}

// auth creates, stores, and returns a new auth token.
func (s *WebServer) auth() string {
	// Create a token to identify the user. Only one token can be active at
	// a time = only 1 authorized device at a time.
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	s.mtx.Lock()
	s.authToken = token
	s.mtx.Unlock()
	return token
}

// token returns the current auth token.
func (s *WebServer) token() string {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.authToken
}

// watchMarket watches the specified market, cancelling any other market
// subscriptions.
func (s *WebServer) watchMarket(dex string, base, quote uint32) (*core.OrderBook, *marketSyncer, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	mktID := marketID(base, quote)

	book, updates, err := s.core.Sync(dex, base, quote)
	if err != nil {
		return nil, nil, err
	}
	s.watchers[mktID]++
	ctx, shutdown := context.WithCancel(s.ctx)
	go func() {
		log.Debugf("monitoring market %d-%d @ %s", base, quote, dex)
	out:
		for {
			select {
			case update := <-updates:
				log.Tracef("order book update received for " + update.Market)
			case <-ctx.Done():
				break out
			}
		}
		// Decrement the watchers, and unsync the book if there are none left.
		s.mtx.Lock()
		defer s.mtx.Unlock()
		s.watchers[mktID]--
		if s.watchers[mktID] == 0 {
			s.core.Unsync(dex, base, quote)
		}
	}()
	syncer := &marketSyncer{
		dex:   dex,
		base:  base,
		quote: quote,
		kill:  shutdown,
	}
	return book, syncer, nil
}

// readPost unmarshals the request body into the provided interface.
func readPost(w http.ResponseWriter, r *http.Request, thing interface{}) bool {
	body, err := ioutil.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		log.Debugf("Error reading request body: %v", err)
		http.Error(w, "error reading JSON message", http.StatusBadRequest)
		return false
	}
	err = json.Unmarshal(body, &thing)
	if err != nil {
		log.Debugf("failed to unmarshal JSON request: %v", err)
		http.Error(w, "failed to unmarshal JSON request", http.StatusBadRequest)
		return false
	}
	return true
}

// userInfo is information about the connected user. Some fields are exported
// for template use.
type userInfo struct {
	authed   bool
	DarkMode bool
}

// Extract the userInfo from the request context.
func extractUserInfo(r *http.Request) *userInfo {
	ai, ok := r.Context().Value(authCV).(*userInfo)
	if !ok {
		log.Errorf("no auth info retrieved from client")
		return &userInfo{}
	}
	return ai
}

// authMiddleware checks incoming requests for cookie-based information
// including the auth token.
func (s *WebServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var authToken string
		cookie, err := r.Cookie(authCK)
		switch err {
		case nil:
			authToken = cookie.Value
		case http.ErrNoCookie:
		default:
			log.Errorf("authToken retrieval error: %v", err)
		}
		authed := authToken != "" && authToken == s.token()

		// darkMode cookie.
		var darkMode bool
		cookie, err = r.Cookie(darkModeCK)
		switch err {
		// Dark mode is the default
		case nil, http.ErrNoCookie:
			darkMode = cookie.Value == "1"
		default:
			log.Errorf("Cookie dcrdataDarkBG retrieval error: %v", err)
		}
		ctx := context.WithValue(r.Context(), authCV, &userInfo{
			authed:   authed,
			DarkMode: darkMode,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// FileServer sets up a http.FileServer handler to serve static files from a
// path on the file system. Directory listings are denied, as are URL paths
// containing "..".
func fileServer(r chi.Router, pathRoot, fsRoot string) {
	if strings.ContainsAny(pathRoot, "{}*") {
		panic("FileServer does not permit URL parameters.")
	}

	// Define a http.HandlerFunc to serve files but not directory indexes.
	hf := func(w http.ResponseWriter, r *http.Request) {
		// Ensure the path begins with "/".
		upath := r.URL.Path
		if strings.Contains(upath, "..") {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(upath, "/") {
			upath = "/" + upath
			r.URL.Path = upath
		}
		// Strip the path prefix and clean the path.
		upath = path.Clean(strings.TrimPrefix(upath, pathRoot))

		// Deny directory listings (http.ServeFile recognizes index.html and
		// attempts to serve the directory contents instead).
		if strings.HasSuffix(upath, "/index.html") {
			http.NotFound(w, r)
			return
		}

		// Generate the full file system path and test for existence.
		fullFilePath := filepath.Join(fsRoot, upath)
		fi, err := os.Stat(fullFilePath)
		if err != nil {
			http.NotFound(w, r)
			return
		}

		// Deny directory listings
		if fi.IsDir() {
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		http.ServeFile(w, r, fullFilePath)
	}

	// For the chi.Mux, make sure a path that ends in "/" and append a "*".
	muxRoot := pathRoot
	if pathRoot != "/" && pathRoot[len(pathRoot)-1] != '/' {
		r.Get(pathRoot, http.RedirectHandler(pathRoot+"/", 301).ServeHTTP)
		muxRoot += "/"
	}
	muxRoot += "*"

	// Mount the http.HandlerFunc on the pathRoot.
	r.Get(muxRoot, hf)
}

// writeJSON marshals the provided interface and writes the bytes to the
// ResponseWriter. The response code is assumed to be StatusOK.
func writeJSON(w http.ResponseWriter, thing interface{}, indent bool) {
	writeJSONWithStatus(w, thing, http.StatusOK, indent)
}

// writeJSON writes marshals the provided interface and writes the bytes to the
// ResponseWriter with the specified response code.
func writeJSONWithStatus(w http.ResponseWriter, thing interface{}, code int, indent bool) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	encoder := json.NewEncoder(w)
	indentStr := ""
	if indent {
		indentStr = "    "
	}
	encoder.SetIndent("", indentStr)
	if err := encoder.Encode(thing); err != nil {
		log.Infof("JSON encode error: %v", err)
	}
}

// filesExists reports whether the named file or directory exists.
func fileExists(name string) bool {
	_, err := os.Stat(name)
	return !os.IsNotExist(err)
}

func marketID(base, quote uint32) string {
	return strconv.Itoa(int(base)) + "_" + strconv.Itoa(int(quote))
}
