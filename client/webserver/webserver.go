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
	"decred.org/dcrdex/client/websocket"
	"decred.org/dcrdex/dex"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// contextKey is the key param type used when saving values to a context using
// context.WithValue. A custom type is defined because built-in types are
// discouraged.
type contextKey string

const (
	// httpConnTimeoutSeconds is the maximum number of seconds allowed for
	// reading an http request or writing the response, beyond which the http
	// connection is terminated.
	httpConnTimeoutSeconds = 10
	// darkModeCK is the cookie key for dark mode.
	darkModeCK = "darkMode"
	// authCK is the authorization token cookie key.
	authCK = "dexauth"
	// popupsCK is the cookie key for the user's preference for showing popups.
	popupsCK = "popups"
	// ctxKeyUserInfo is used in the authorization middleware for saving user
	// info in http request contexts.
	ctxKeyUserInfo = contextKey("userinfo")
	// notifyRoute is a route used for general notifications.
	notifyRoute = "notify"
	// The basis for content-security-policy. connect-src must be the final
	// directive so that it can be reliably supplemented on startup.
	baseCSP = "default-src 'none'; script-src 'self'; img-src 'self'; style-src 'self'; font-src 'self'; connect-src 'self'"
)

var (
	log   dex.Logger
	unbip = dex.BipIDSymbol
)

// clientCore is satisfied by core.Core.
type clientCore interface {
	websocket.Core
	Network() dex.Network
	Exchanges() map[string]*core.Exchange
	Register(*core.RegisterForm) (*core.RegisterResult, error)
	Login(pw []byte) (*core.LoginResult, error)
	InitializeClient(pw []byte) error
	AssetBalance(assetID uint32) (*core.WalletBalance, error)
	CreateWallet(appPW, walletPW []byte, form *core.WalletForm) error
	OpenWallet(assetID uint32, pw []byte) error
	CloseWallet(assetID uint32) error
	ConnectWallet(assetID uint32) error
	Wallets() []*core.WalletState
	WalletState(assetID uint32) *core.WalletState
	WalletSettings(uint32) (map[string]string, error)
	ReconfigureWallet([]byte, []byte, uint32, map[string]string) error
	ChangeAppPass([]byte, []byte) error
	NewDepositAddress(assetID uint32) (string, error)
	AutoWalletConfig(assetID uint32) (map[string]string, error)
	User() *core.User
	GetFee(url string, cert interface{}) (uint64, error)
	SupportedAssets() map[uint32]*core.SupportedAsset
	Withdraw(pw []byte, assetID uint32, value uint64, address string) (asset.Coin, error)
	Trade(pw []byte, form *core.TradeForm) (*core.Order, error)
	Cancel(pw []byte, oid dex.Bytes) error
	NotificationFeed() <-chan core.Notification
	Logout() error
	Orders(*core.OrderFilter) ([]*core.Order, error)
	Order(oid dex.Bytes) (*core.Order, error)
	MaxBuy(host string, base, quote uint32, rate uint64) (*core.MaxOrderEstimate, error)
	MaxSell(host string, base, quote uint32) (*core.MaxOrderEstimate, error)
	AccountExport(pw []byte, host string) (*core.Account, error)
	AccountImport(pw []byte, account core.Account) error
	AccountDisable(pw []byte, host string) error
	IsInitialized() (bool, error)
}

var _ clientCore = (*core.Core)(nil)

// WebServer is a single-client http and websocket server enabling a browser
// interface to the DEX client.
type WebServer struct {
	wsServer   *websocket.Server
	mux        *chi.Mux
	core       clientCore
	addr       string
	csp        string
	srv        *http.Server
	html       *templates
	indent     bool
	mtx        sync.RWMutex
	authTokens map[string]bool
}

// New is the constructor for a new WebServer. customSiteDir can be left blank,
// in which case a handful of default locations will be checked. This will work
// in most cases.
func New(core clientCore, addr, customSiteDir string, logger dex.Logger, reloadHTML, httpProf bool) (*WebServer, error) {
	log = logger

	folderExists := func(fp string) bool {
		stat, err := os.Stat(fp)
		return err == nil && stat.IsDir()
	}

	// Look for the "site" folder in the executable's path, the working
	// directory, or the source path relative to [repo root]/client/cmd/dexc.
	execPath, err := os.Executable() // e.g. /usr/bin/dexc
	if err != nil {
		return nil, fmt.Errorf("unable to locate executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath) // e.g. /opt/decred/dex/dexc
	if err != nil {
		return nil, fmt.Errorf("unable to locate executable path: %w", err)
	}
	execPath = filepath.Dir(execPath) // e.g. /opt/decred/dex

	// executable path
	var siteDir string
	absDir, _ := filepath.Abs("site")
	for _, dir := range []string{
		customSiteDir,
		filepath.Join(execPath, "site"),
		absDir,
		filepath.Clean(filepath.Join(execPath, "../../webserver/site")),
	} {
		log.Debugf("Looking for site in %s", dir)
		if folderExists(dir) {
			siteDir = dir
			break
		}
	}

	if siteDir == "" {
		return nil, fmt.Errorf("no HTML template files found. "+
			"Place the 'site' folder in the executable's directory %q or the working directory, "+
			"or run dexc from within the client/cmd/dexc source workspace folder, or specify the"+
			"'sitedir' configuration directive to dexc.", execPath)
	}
	log.Infof("Located \"site\" folder at %v", siteDir)

	// Prepare the templates.
	bb := "bodybuilder"
	tmpl := newTemplates(filepath.Join(siteDir, "src/html"), reloadHTML).
		addTemplate("login", bb).
		addTemplate("register", bb, "forms").
		addTemplate("markets", bb, "forms").
		addTemplate("wallets", bb, "forms").
		addTemplate("settings", bb, "forms").
		addTemplate("orders", bb).
		addTemplate("order", bb, "forms")
	err = tmpl.buildErr()
	if err != nil {
		return nil, err
	}

	// Create an HTTP router.
	mux := chi.NewRouter()
	httpServer := &http.Server{
		Handler:      mux,
		ReadTimeout:  httpConnTimeoutSeconds * time.Second, // slow requests should not hold connections opened
		WriteTimeout: 2 * time.Minute,                      // request to response time, must be long enough for slow handlers
	}

	// Make the server here so its methods can be registered.
	s := &WebServer{
		core:       core,
		mux:        mux,
		srv:        httpServer,
		addr:       addr,
		html:       tmpl,
		wsServer:   websocket.New(core, log.SubLogger("WS")),
		authTokens: make(map[string]bool),
	}

	// Middleware
	if log.Level() == dex.LevelTrace {
		mux.Use(middleware.Logger)
	}
	mux.Use(s.securityMiddleware)
	mux.Use(middleware.Recoverer)
	mux.Use(s.authMiddleware)

	// HTTP profiler
	if httpProf {
		profPath := "/debug/pprof"
		log.Infof("Mounting the HTTP profiler on %s", profPath)
		// Option A: mount each httpprof handler directly. The caveat with this
		// is that httpprof.Index ONLY works when mounted on /debug/pprof/.
		//
		// mux.Mount(profPath, http.HandlerFunc(httppprof.Index)) // also
		// handles: goroutine, heap, threadcreate, block, allocs, mutex
		// mux.Mount(profPath+"/cmdline", http.HandlerFunc(httppprof.Cmdline))
		// mux.Mount(profPath+"/profile", http.HandlerFunc(httppprof.Profile))
		// mux.Mount(profPath+"/symbol", http.HandlerFunc(httppprof.Symbol))
		// mux.Mount(profPath+"/trace", http.HandlerFunc(httppprof.Trace))

		// Option B: http pprof uses http.DefaultServeMux, so mount it:
		mux.Mount(profPath, http.DefaultServeMux) // profPath MUST be /debug/pprof this way
	}

	// The WebSocket handler is mounted on /ws in Connect.

	// Webpages
	mux.Group(func(web chi.Router) {
		// The register page and settings page are always allowed.
		// The register page performs init if needed, along with
		// initial setup and settings is used to register more DEXs
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
				webAuth.With(orderIDCtx).Get("/order/{oid}", s.handleOrder)
				webAuth.Get(ordersRoute, s.handleOrders)

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
		r.Post("/init", s.apiInit)
		r.Get("/isinitialized", s.apiIsInitialized)

		r.Group(func(apiInit chi.Router) {
			apiInit.Use(s.rejectUninited)
			apiInit.Post("/login", s.apiLogin)
			apiInit.Post("/getfee", s.apiGetFee)
		})

		r.Group(func(apiAuth chi.Router) {
			apiAuth.Use(s.rejectUnauthed)
			apiAuth.Get("/user", s.apiUser)
			apiAuth.Post("/defaultwalletcfg", s.apiDefaultWalletCfg)
			apiAuth.Post("/register", s.apiRegister)
			apiAuth.Post("/newwallet", s.apiNewWallet)
			apiAuth.Post("/openwallet", s.apiOpenWallet)
			apiAuth.Post("/depositaddress", s.apiNewDepositAddress)
			apiAuth.Post("/closewallet", s.apiCloseWallet)
			apiAuth.Post("/connectwallet", s.apiConnectWallet)
			apiAuth.Post("/trade", s.apiTrade)
			apiAuth.Post("/cancel", s.apiCancel)
			apiAuth.Post("/logout", s.apiLogout)
			apiAuth.Post("/balance", s.apiGetBalance)
			apiAuth.Post("/parseconfig", s.apiParseConfig)
			apiAuth.Post("/reconfigurewallet", s.apiReconfig)
			apiAuth.Post("/changeapppass", s.apiChangeAppPass)
			apiAuth.Post("/walletsettings", s.apiWalletSettings)
			apiAuth.Post("/orders", s.apiOrders)
			apiAuth.Post("/order", s.apiOrder)
			apiAuth.Post("/withdraw", s.apiWithdraw)
			apiAuth.Post("/maxbuy", s.apiMaxBuy)
			apiAuth.Post("/maxsell", s.apiMaxSell)
			apiAuth.Post("/exportaccount", s.apiAccountExport)
			apiAuth.Post("/importaccount", s.apiAccountImport)
			apiAuth.Post("/disableaccount", s.apiAccountDisable)
		})
	})

	// Files
	fileServer(mux, "/js", filepath.Join(siteDir, "dist"), "text/javascript")
	fileServer(mux, "/css", filepath.Join(siteDir, "dist"), "text/css")
	fileServer(mux, "/img", filepath.Join(siteDir, "src/img"), "")
	fileServer(mux, "/font", filepath.Join(siteDir, "src/font"), "")

	return s, nil
}

// Connect starts the web server. Satisfies the dex.Connector interface.
func (s *WebServer) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	// Start serving.
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return nil, fmt.Errorf("Can't listen on %s. web server quitting: %w", s.addr, err)
	}
	// Update the listening address in case a :0 was provided.
	s.addr = listener.Addr().String()
	// Work around a webkit (safari) bug with the handling of the connect-src
	// directive of content security policy. See:
	// https://bugs.webkit.org/show_bug.cgi?id=201591
	s.csp = fmt.Sprintf("%s ws://%s", baseCSP, s.addr)

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

	// Configure the websocket handler before starting the server.
	s.mux.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		s.wsServer.HandleConnect(ctx, w, r)
	})

	wg.Add(1)
	go func() {
		defer wg.Done()
		err = s.srv.Serve(listener)
		if !errors.Is(err, http.ErrServerClosed) {
			log.Warnf("unexpected (http.Server).Serve error: %v", err)
		}
		// Disconnect the websocket clients since http.(*Server).Shutdown does
		// not deal with hijacked websocket connections.
		s.wsServer.Shutdown()
		log.Infof("Web server off")
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.readNotifications(ctx)
	}()

	log.Infof("Web server listening on %s", s.addr)
	fmt.Printf("\n\t****  OPEN IN YOUR BROWSER TO LOGIN AND TRADE  --->  http://%s  ****\n\n", s.addr)
	return &wg, nil
}

// authorize creates, stores, and returns a new auth token to identify the user.
// deauth should be used to invalidate tokens on logout.
func (s *WebServer) authorize() string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	s.mtx.Lock()
	s.authTokens[token] = true
	s.mtx.Unlock()
	return token
}

// deauth invalidates all current auth tokens. All existing sessions will need
// to login again.
func (s *WebServer) deauth() {
	s.mtx.Lock()
	s.authTokens = make(map[string]bool)
	s.mtx.Unlock()
}

// isAuthed checks if the incoming request is from an authorized user/device.
// Requires the auth token cookie to be set in the request and for the token
// to match `WebServer.validAuthToken`.
func (s *WebServer) isAuthed(r *http.Request) bool {
	var authToken string
	cookie, err := r.Cookie(authCK)
	switch {
	case err == nil:
		authToken = cookie.Value
	case errors.Is(err, http.ErrNoCookie):
	default:
		log.Errorf("authToken retrieval error: %v", err)
	}

	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.authTokens[authToken]
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
	Authed     bool
	DarkMode   bool
	ShowPopups bool
}

// Extract the userInfo from the request context. This should be used with
// authMiddleware.
func extractUserInfo(r *http.Request) *userInfo {
	user, ok := r.Context().Value(ctxKeyUserInfo).(*userInfo)
	if !ok {
		log.Errorf("no auth info retrieved from client")
		return &userInfo{}
	}
	return user
}

// fileServer sets up a http.FileServer handler to serve static files from a
// path on the file system. Directory listings are denied, as are URL paths
// containing "..".
func fileServer(r chi.Router, pathRoot, fsRoot, contentType string) {
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

		// Ran into a Windows quirk where net was setting the content-type
		// incorrectly based on a bad registry value or something. This should
		// prevent that. It's most important for javascript files, because we
		// add a nosniff header and the browser would refuse to execute a js
		// file with the wrong header.
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
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
