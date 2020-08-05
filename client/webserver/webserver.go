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
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/websocket"
	"decred.org/dcrdex/dex"
	"github.com/decred/slog"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
)

// contextKey is the key param type used when saving values to a context using
// context.WithValue. A custom type is defined because built-in types are
// discouraged.
type contextKey string

const (
	// httpConnTimeoutSeconds is the maximum number of seconds allowed for reading
	// an http request or writing the response, beyond which the http connection is
	// terminated.
	httpConnTimeoutSeconds = 10
	// darkModeCK is the cookie key for dark mode.
	darkModeCK = "darkMode"
	// authCK is the authorization token cookie key.
	authCK = "dexauth"
	// ctxKeyUserInfo is used in the authorization middleware for saving user
	// info in http request contexts.
	ctxKeyUserInfo = contextKey("userinfo")
	// notifyRoute is a route used for general notifications.
	notifyRoute = "notify"
)

var (
	// Time allowed to read the next pong message from the peer. The
	// default is intended for production, but leaving as a var instead of const
	// to facilitate testing.
	pongWait = 60 * time.Second
	log      slog.Logger
	unbip    = dex.BipIDSymbol
)

// clientCore is satisfied by core.Core.
type clientCore interface {
	Exchanges() map[string]*core.Exchange
	Register(*core.RegisterForm) (*core.RegisterResult, error)
	Login(pw []byte) (*core.LoginResult, error)
	InitializeClient(pw []byte) error
	AssetBalance(assetID uint32) (*db.Balance, error)
	WalletState(assetID uint32) *core.WalletState
	CreateWallet(appPW, walletPW []byte, form *core.WalletForm) error
	OpenWallet(assetID uint32, pw []byte) error
	CloseWallet(assetID uint32) error
	ConnectWallet(assetID uint32) error
	Wallets() []*core.WalletState
	WalletSettings(uint32) (map[string]string, error)
	ReconfigureWallet([]byte, uint32, map[string]string) error
	SetWalletPassword(appPW []byte, assetID uint32, newPW []byte) error
	User() *core.User
	GetFee(url, cert string) (uint64, error)
	SupportedAssets() map[uint32]*core.SupportedAsset
	Withdraw(pw []byte, assetID uint32, value uint64, address string) (asset.Coin, error)
	Trade(pw []byte, form *core.TradeForm) (*core.Order, error)
	Cancel(pw []byte, sid string) error
	NotificationFeed() <-chan core.Notification
	Logout() error
}

var _ clientCore = (*core.Core)(nil)

// WebServer is a single-client http and websocket server enabling a browser
// interface to the DEX client.
type WebServer struct {
	wsServer       *websocket.Server
	core           clientCore
	addr           string
	srv            *http.Server
	html           *templates
	indent         bool
	mtx            sync.RWMutex
	validAuthToken string
}

// New is the constructor for a new WebServer.
func New(core clientCore, addr string, wsServer *websocket.Server, logger slog.Logger, reloadHTML bool) (*WebServer, error) {
	log = logger

	folderExists := func(fp string) bool {
		stat, err := os.Stat(fp)
		return err == nil && stat.IsDir()
	}

	// Right now, it is expected that the working directory
	// is either the dcrdex root directory, or the WebServer directory itself.
	root := "../../webserver/site"
	if !folderExists(root) {
		root = "site"
		if !folderExists(root) {
			return nil, fmt.Errorf("no HTML template files found")
		}
	}

	join := filepath.Join

	// Prepare the templates.
	bb := "bodybuilder"
	tmpl := newTemplates(join(root, "src/html"), reloadHTML).
		addTemplate("login", bb).
		addTemplate("register", bb, "forms").
		addTemplate("markets", bb, "forms").
		addTemplate("wallets", bb, "forms").
		addTemplate("settings", bb, "forms")
	err := tmpl.buildErr()
	if err != nil {
		return nil, err
	}

	// Create an HTTP router.
	mux := chi.NewRouter()
	httpServer := &http.Server{
		Handler:      mux,
		ReadTimeout:  httpConnTimeoutSeconds * time.Second, // slow requests should not hold connections opened
		WriteTimeout: httpConnTimeoutSeconds * time.Second, // hung responses must die
	}

	// Make the server here so its methods can be registered.
	s := &WebServer{
		core:     core,
		srv:      httpServer,
		addr:     addr,
		html:     tmpl,
		wsServer: wsServer,
	}

	// Middleware
	mux.Use(securityMiddleware)
	mux.Use(middleware.Recoverer)
	mux.Use(s.authMiddleware)

	// Websocket endpoint
	mux.Get("/ws", s.wsServer.HandleConnect)

	// Webpages
	mux.Group(func(web chi.Router) {
		// The register page and settings page are always allowed.
		// The register page performs init if needed, along with
		// initial setup and is also used to register more DEXs
		// after initial setup.
		web.Get(registerRoute, s.handleRegister)
		web.Get(settingsRoute, s.handleSettings)

		// The rest of the web handlers require initialization.
		web.Group(func(webInit chi.Router) {
			webInit.Use(s.requireInit)
			// The login handler is the only one that requires init but not auth
			// since it performs the auth.
			webInit.Get(loginRoute, s.handleLogin)

			// The rest of these handlers require auth.
			webInit.Group(func(webAuth chi.Router) {
				webAuth.Use(s.requireLogin)
				webAuth.Get(walletsRoute, s.handleWallets)

				// These handlers require a DEX connection.
				webAuth.Group(func(webDC chi.Router) {
					webDC.Use(s.requireDEXConnection)
					webDC.Get(homeRoute, s.handleHome)
					webDC.Get(marketsRoute, s.handleMarkets)
				})
			})
		})
	})

	// api endpoints
	mux.Route("/api", func(r chi.Router) {
		r.Use(middleware.AllowContentType("application/json"))
		r.Post("/getfee", s.apiGetFee)
		r.Post("/newwallet", s.apiNewWallet)
		r.Post("/openwallet", s.apiOpenWallet)
		r.Post("/closewallet", s.apiCloseWallet)
		r.Post("/register", s.apiRegister)
		r.Post("/init", s.apiInit)
		r.Post("/login", s.apiLogin)
		r.Post("/withdraw", s.apiWithdraw)
		r.Get("/user", s.apiUser)
		r.Post("/connectwallet", s.apiConnectWallet)
		r.Post("/trade", s.apiTrade)
		r.Post("/cancel", s.apiCancel)
		r.Post("/logout", s.apiLogout)
		r.Post("/balance", s.apiGetBalance)
		r.Post("/parseconfig", s.apiParseConfig)
		r.Post("/reconfigurewallet", s.apiReconfig)
		r.Post("/walletsettings", s.apiWalletSettings)
		r.Post("/setwalletpass", s.apiSetWalletPass)
	})

	// Files
	fileServer(mux, "/js", join(root, "dist"))
	fileServer(mux, "/css", join(root, "dist"))
	fileServer(mux, "/img", join(root, "src/img"))
	fileServer(mux, "/font", join(root, "src/font"))

	return s, nil
}

// Connect starts the web server. Satisfies the dex.Connector interface.
func (s *WebServer) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	// Start serving.
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return nil, fmt.Errorf("Can't listen on %s. web server quitting: %v", s.addr, err)
	}
	// Update the listening address in case a :0 was provided.
	s.addr = listener.Addr().String()

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
		err = s.srv.Serve(listener)
		if !errors.Is(err, http.ErrServerClosed) {
			log.Warnf("unexpected (http.Server).Serve error: %v", err)
		}
		log.Infof("Web server off")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.readNotifications(ctx)
	}()

	log.Infof("Web server listening on http://%s", s.addr)
	return &wg, nil
}

// authorize creates, stores, and returns a new auth token to identify the
// user. Only one token can be active at a time = only 1 authorized device
// at a time. `WebServer.validAuthToken` is set to the newly created token,
// effectively deauthorizing previously authenticated devices.
func (s *WebServer) authorize() string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	s.mtx.Lock()
	s.validAuthToken = token
	s.mtx.Unlock()
	return token
}

// isAuthed checks if the incoming request is from an authorized user/device.
// Requires the auth token cookie to be set in the request and for the token
// to match `WebServer.validAuthToken`.
func (s *WebServer) isAuthed(r *http.Request) bool {
	var authToken string
	cookie, err := r.Cookie(authCK)
	switch err {
	case nil:
		authToken = cookie.Value
	case http.ErrNoCookie:
	default:
		log.Errorf("authToken retrieval error: %v", err)
	}

	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return authToken != "" && authToken == s.validAuthToken
}

// readNotifications reads from the Core notification channel and relays to
// websocket clients.
func (s *WebServer) readNotifications(ctx context.Context) {
	ch := s.core.NotificationFeed()
	for {
		select {
		case n := <-ch:
			s.wsServer.Notify(notifyRoute, n)
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
	user, ok := r.Context().Value(ctxKeyUserInfo).(*userInfo)
	if !ok {
		log.Errorf("no auth info retrieved from client")
		return &userInfo{}
	}
	return user
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
