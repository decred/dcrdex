// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
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

var (
	log slog.Logger
	// updateWalletRoute is a notification route that updates the state of a
	// wallet.
	updateWalletRoute = "update_wallet"
	// notifyRoute is a route used for general notifications.
	notifyRoute = "notify"
)

// clientCore is satisfied by core.Core.
type clientCore interface {
	Exchanges() map[string]*core.Exchange
	Register(*core.RegisterForm) error
	Login(pw string) ([]*db.Notification, error)
	InitializeClient(pw string) error
	Sync(dex string, base, quote uint32) (*core.OrderBook, *core.BookFeed, error)
	Balance(uint32) (uint64, error)
	WalletState(assetID uint32) *core.WalletState
	CreateWallet(appPW, walletPW string, form *core.WalletForm) error
	OpenWallet(assetID uint32, pw string) error
	CloseWallet(assetID uint32) error
	ConnectWallet(assetID uint32) error
	Wallets() []*core.WalletState
	User() *core.User
	PreRegister(*core.PreRegisterForm) (uint64, error)
	SupportedAssets() map[uint32]*core.SupportedAsset
	Withdraw(pw string, assetID uint32, value uint64) (asset.Coin, error)
	Trade(pw string, form *core.TradeForm) (*core.Order, error)
	Cancel(pw string, sid string) error
	NotificationFeed() <-chan core.Notification
	AckNotes([]dex.Bytes)
}

// marketSyncer is used to synchronize market subscriptions. The marketSyncer
// manages a map of clients who are subscribed to the market, and distributes
// order book updates when received.
type marketSyncer struct {
	feed *core.BookFeed
	cl   *wsClient
}

// newMarketSyncer is the constructor for a marketSyncer, returned as a running
// *dex.StartStopWaiter.
func newMarketSyncer(ctx context.Context, cl *wsClient, feed *core.BookFeed) *dex.StartStopWaiter {
	ssWaiter := dex.NewStartStopWaiter(&marketSyncer{
		feed: feed,
		cl:   cl,
	})
	ssWaiter.Start(ctx)
	return ssWaiter
}

func (m *marketSyncer) Run(ctx context.Context) {
	defer m.feed.Close()
out:
	for {
		select {
		case update := <-m.feed.C:
			note, err := msgjson.NewNotification(update.Action, update)
			if err != nil {
				log.Errorf("error encoding notification message: %v", err)
				break out
			}
			err = m.cl.Send(note)
			if err != nil {
				log.Debug("send error. ending market feed")
				break out
			}
		case <-ctx.Done():
			break out
		}
	}
}

// WebServer is a single-client http and websocket server enabling a browser
// interface to the DEX client.
type WebServer struct {
	ctx       context.Context
	core      clientCore
	addr      string
	srv       *http.Server
	html      *templates
	indent    bool
	mtx       sync.RWMutex
	authToken string
	syncers   map[string]*marketSyncer
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
	root := "../../webserver/site"
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
		addTemplate("register", bb, "forms").
		addTemplate("markets", bb, "forms").
		addTemplate("wallets", bb, "forms").
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

	// Make the server here so its methods can be registered.
	s := &WebServer{
		core:    core,
		srv:     httpServer,
		addr:    addr,
		html:    tmpl,
		syncers: make(map[string]*marketSyncer),
		clients: make(map[int32]*wsClient),
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
		r.Post("/preregister", s.apiPreRegister)
		r.Post("/newwallet", s.apiNewWallet)
		r.Post("/openwallet", s.apiOpenWallet)
		r.Post("/closewallet", s.apiCloseWallet)
		r.Post("/register", s.apiRegister)
		r.Post("/init", s.apiInit)
		r.Post("/login", s.apiLogin)
		r.Post("/withdraw", s.apiWithdraw)
		r.Get("/user", s.apiUser)
		r.Post("/connectwallet", s.apiConnect)
		r.Post("/trade", s.apiTrade)
		r.Post("/cancel", s.apiCancel)
	})
	// Files
	fileServer(mux, "/js", fp(root, "dist"))
	fileServer(mux, "/css", fp(root, "dist"))
	fileServer(mux, "/img", fp(root, "src/img"))
	fileServer(mux, "/font", fp(root, "src/font"))

	return s, nil
}

// Run starts the web server. Satisfies the dex.Runner interface.
func (s *WebServer) Run(ctx context.Context) {
	// We'll use the context for market syncers.
	s.ctx = ctx
	// Start serving.
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		log.Errorf("Can't listen on %s. web server quitting: %v", s.addr, err)
		return
	}

	// Shutdown the server on context cancellation.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		err := s.srv.Shutdown(context.Background())
		if err != nil {
			log.Errorf("Problem shutting down rpc: %v", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.readNotifications(ctx)
	}()

	log.Infof("Web server listening on http://%s", s.addr)
	err = s.srv.Serve(listener)
	if !errors.Is(err, http.ErrServerClosed) {
		log.Warnf("unexpected (http.Server).Serve error: %v", err)
	}
	log.Infof("Web server off")

	// Disconnect the websocket clients since Shutdown does not deal with
	// hijacked websocket connections.
	s.mtx.Lock()
	defer s.mtx.Unlock()
	for _, cl := range s.clients {
		cl.Disconnect()
	}

	wg.Wait()
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

// readNotifications reads from the Core notification channel and relays to
// websocket clients.
func (s *WebServer) readNotifications(ctx context.Context) {
	ch := s.core.NotificationFeed()
	for {
		select {
		case n := <-ch:
			s.notify(notifyRoute, n)
			// log.Trace("%s: %s: %s", n.Severity(), n.Subject(), n.Details())
		case <-ctx.Done():
			return
		}
	}
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
	err = json.Unmarshal(body, thing)
	if err != nil {
		log.Debugf("failed to unmarshal JSON request: %v", err)
		http.Error(w, "failed to unmarshal JSON request", http.StatusBadRequest)
		return false
	}
	return true
}

// userInfo is information about the connected user. This type embeds the
// core.User type, adding fields specific to the users server authentication
// and cookies.
type userInfo struct {
	*core.User
	Authed   bool
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
		case nil:
			darkMode = cookie.Value == "1"
		case http.ErrNoCookie:
			darkMode = true
		default:
			log.Errorf("Cookie dark mode retrieval error: %v", err)
		}
		ctx := context.WithValue(r.Context(), authCV, &userInfo{
			User:     s.core.User(),
			Authed:   authed,
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

// Create a unique ID for a market.
func marketID(base, quote uint32) string {
	return strconv.Itoa(int(base)) + "_" + strconv.Itoa(int(quote))
}
