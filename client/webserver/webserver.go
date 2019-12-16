// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/ws"
	"github.com/decred/slog"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/gorilla/websocket"
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

// clientCore is satisfied by core.Core.
type clientCore interface {
	ListMarkets() []*core.MarketInfo
	Register(*core.Registration) error
	Login(dex, pw string) error
	Sync(dex, mkt string) (*core.OrderBook, chan *core.BookUpdate, error)
	Unsync(dex, mkt string)
	Balance(string) (float64, error)
}

type marketSyncer struct {
	market string
	kill   func()
}

// webServer is a single-client http and websocket server enabling a browser
// interface to the DEX client.
type webServer struct {
	ctx        context.Context
	core       clientCore
	html       *templates
	indent     bool
	mtx        sync.RWMutex
	authToken  string
	openMarket *marketSyncer
}

// Run is the single exported function of webserver. Run the server, listening
// on the provided network address until the context is canceled.
func Run(ctx context.Context, core clientCore, addr string, logger slog.Logger) {
	log = logger

	folderExists := func(fp string) bool {
		stat, err := os.Stat(fp)
		return err == nil && stat.IsDir()
	}
	fp := filepath.Join

	// Right now, it is expected that the working directory
	// is either the dcrdex root directory, or the webserver directory itself.
	root := "client/webserver/site"
	if !folderExists(root) {
		root = "site"
		if !folderExists(root) {
			log.Errorf("no HTML template files found")
			return
		}
	}

	// Prepare the templates.
	bb := "bodybuilder"
	tmpl := newTemplates(fp(root, "src/html"), true).
		addTemplate("login", bb).
		addTemplate("register", bb).
		addTemplate("markets", bb).
		addTemplate("wallets", bb).
		addTemplate("settings", bb)
	err := tmpl.buildErr()
	if err != nil {
		log.Errorf(err.Error())
		return
	}

	// Create an HTTP router, putting a couple of useful middlewares in place.
	mux := chi.NewRouter()
	httpServer := &http.Server{
		Handler:      mux,
		ReadTimeout:  rpcTimeoutSeconds * time.Second, // slow requests should not hold connections opened
		WriteTimeout: rpcTimeoutSeconds * time.Second, // hung responses must die
	}

	// Make the server here so its methods can be registered.
	s := &webServer{
		ctx:  ctx,
		core: core,
		html: tmpl,
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

	// Start serving.
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Errorf("Can't listen on %s: %v. web server quitting", addr, err)
		return
	}
	// Close the listener on context cancellation.
	go func() {
		<-ctx.Done()
		err := listener.Close()
		if err != nil {
			log.Errorf("Problem shutting down rpc: %v", err)
		}
	}()
	log.Infof("Web server listening on %s", listener.Addr())
	err = httpServer.Serve(listener)
	if err != http.ErrServerClosed {
		log.Warnf("unexpected (http.Server).Serve error: %v", err)
	}
	log.Infof("Web server off")
}

func (s *webServer) handleWS(w http.ResponseWriter, r *http.Request) {
	// If the IP address includes a port, remove it.
	ip := r.RemoteAddr
	// If a host:port can be parsed, the IP is only the host portion.
	host, _, err := net.SplitHostPort(ip)
	if err == nil && host != "" {
		ip = host
	}
	wsConn, err := ws.NewConnection(w, r, pingPeriod+pongWait)
	if err != nil {
		log.Errorf("ws connection error: %v", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	go s.websocketHandler(wsConn, ip)
}

// websocketHandler handles a new websocket client by creating a new wsClient,
// starting it, and blocking until the connection closes. This method should be
// run as a goroutine.
func (s *webServer) websocketHandler(conn ws.Connection, ip string) {
	log.Tracef("New websocket client %s", ip)
	// Create a new websocket client to handle the new websocket connection
	// and wait for it to shutdown.  Once it has shutdown (and hence
	// disconnected), remove it.
	var wsLink *ws.WSLink
	wsLink = ws.NewWSLink(ip, conn, pingPeriod, func(msg *msgjson.Message) *msgjson.Error {
		return s.handleMessage(wsLink, msg)
	})
	wsLink.Start()
	wsLink.WaitForShutdown()
	log.Tracef("Disconnected websocket client %s", ip)
}

// auth creates, stores, and returns a new auth token.
func (s *webServer) auth() string {
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
func (s *webServer) token() string {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.authToken
}

// watchMarket watches the specified market, cancelling any other market
// subscriptions.
func (s *webServer) watchMarket(dex, mkt string) (*core.OrderBook, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	old := s.openMarket
	if old != nil {
		log.Tracef("ending monitoring of market %s", old.market)
		old.kill()
	}
	book, updates, err := s.core.Sync(dex, mkt)
	if err != nil {
		return nil, err
	}
	ctx, shutdown := context.WithCancel(s.ctx)
	go func() {
		log.Debugf("monitoring market %s @ %s", mkt, dex)
	out:
		for {
			select {
			case update := <-updates:
				log.Tracef("order book update received for " + update.Market)
			case <-ctx.Done():
				break out
			}
		}
		s.core.Unsync(dex, mkt)
	}()
	s.openMarket = &marketSyncer{
		market: mkt,
		kill:   shutdown,
	}
	return book, nil
}

// unmarket kills unsubscribed from any market being monitored.
func (s *webServer) unmarket() {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if s.openMarket != nil {
		log.Tracef("ending monitoring of market %s", s.openMarket.market)
		s.openMarket.kill()
	}
	s.openMarket = nil
}

// handleMessage handles the websocket message, applying the right handler for the
// route.
func (s *webServer) handleMessage(conn *ws.WSLink, msg *msgjson.Message) *msgjson.Error {
	log.Tracef("message of type %d received for route %s", msg.Type, msg.Route)
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

// wsHandlers is the map used by the server to locate the router handler for a
// request.
var wsHandlers = map[string]func(*webServer, *ws.WSLink, *msgjson.Message) *msgjson.Error{
	"loadmarket": wsLoadMarket,
	"unmarket":   wsUnmarket,
}

// marketLoad is sent by websocket clients to subscribe to a market and request
// the order book.
type marketLoad struct {
	DEX    string `json:"dex"`
	Market string `json:"market"`
}

// wsLoadMarket is the handler for the 'loadmarket' websocket endpoint. Sets the
// currently monitored markets and starts the notification feed with the order
// book.
func wsLoadMarket(s *webServer, conn *ws.WSLink, msg *msgjson.Message) *msgjson.Error {
	// The user has loaded the markets page. Get the last known market for the
	// user and send the page information.
	market := new(marketLoad)
	err := json.Unmarshal(msg.Payload, market)
	if err != nil {
		log.Errorf("error unmarshaling marketload payload: %v", err)
		return msgjson.NewError(msgjson.RPCInternal, "error unmarshaling marketload payload: "+err.Error())
	}
	base, quote, err := decodeMarket(market.Market)
	if err != nil {
		return msgjson.NewError(msgjson.UnknownMarketError, err.Error())
	}
	baseBalance, err := s.core.Balance(base)
	if err != nil {
		log.Errorf("unable to get balance for %s", base)
		return msgjson.NewError(msgjson.RPCInternal, "error getting balance for "+base+": "+err.Error())
	}
	quoteBalance, err := s.core.Balance(quote)
	if err != nil {
		log.Errorf("unable to get balance for %s", quote)
		return msgjson.NewError(msgjson.RPCInternal, "error getting balance for "+quote+": "+err.Error())
	}

	// Switch current market.
	book, err := s.watchMarket(market.DEX, market.Market)
	if err != nil {
		log.Errorf("error watching market %s @ %s: %v", market.Market, market.DEX, err)
		return msgjson.NewError(msgjson.RPCInternal, "error watching "+market.Market)
	}

	note, err := msgjson.NewNotification("book", map[string]interface{}{
		"book":         book,
		"market":       market.Market,
		"dex":          market.DEX,
		"base":         base,
		"quote":        quote,
		"baseBalance":  baseBalance,
		"quoteBalance": quoteBalance,
	})
	if err != nil {
		log.Errorf("error encoding loadmarkets response: %v", err)
		return msgjson.NewError(msgjson.RPCInternal, "error encoding order book: "+err.Error())
	}
	conn.Send(note)
	return nil
}

// wsUnmarket is the handler for the 'unmarket' websocket endpoint. This empty
// message is sent when the user leaves the markets page. Unsubscribed from the
// current market.
func wsUnmarket(s *webServer, conn *ws.WSLink, _ *msgjson.Message) *msgjson.Error {
	s.unmarket()
	return nil
}

// sendTemplate processes the template and sends the result.
func (s *webServer) sendTemplate(w http.ResponseWriter, r *http.Request, tmplID string, data interface{}) {
	page, err := s.html.exec(tmplID, data)
	if err != nil {
		log.Errorf("template exec error for %s: %v", tmplID, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, page)
}

func (s *webServer) handleHome(w http.ResponseWriter, r *http.Request) {
	userInfo := extractUserInfo(r)
	var tmplID string
	var data interface{}
	switch {
	case !userInfo.authed:
		tmplID = "login"
		data = commonArgs(r, "Login | Decred DEX")
	default:
		tmplID = "markets"
		data = s.marketResult(r)
	}
	s.sendTemplate(w, r, tmplID, data)
}

// CommonArguments are common page arguments that must be supplied to every
// page to populate the <title> and <header> elements.
type CommonArguments struct {
	UserInfo *userInfo
	Title    string
}

func commonArgs(r *http.Request, title string) *CommonArguments {
	return &CommonArguments{
		UserInfo: extractUserInfo(r),
		Title:    title,
	}
}

// handleLogin
func (s *webServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	s.sendTemplate(w, r, "login", commonArgs(r, "Login | Decred DEX"))
}

func (s *webServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	s.sendTemplate(w, r, "register", commonArgs(r, "Register | Decred DEX"))
}

type marketResult struct {
	CommonArguments
	DEXes []*core.MarketInfo
}

// marketResult returns a *marketResult, which is needed to process the
// 'markets' template.
func (s *webServer) marketResult(r *http.Request) *marketResult {
	return &marketResult{
		CommonArguments: *commonArgs(r, "Markets | Decred DEX"),
		DEXes:           s.core.ListMarkets(),
	}
}

// handleMarkets handles the 'markets' page request.
func (s *webServer) handleMarkets(w http.ResponseWriter, r *http.Request) {
	s.sendTemplate(w, r, "markets", s.marketResult(r))
}

// handleWallets handles the 'wallets' page request.
func (s *webServer) handleWallets(w http.ResponseWriter, r *http.Request) {
	s.sendTemplate(w, r, "wallets", commonArgs(r, "Wallets | Decred DEX"))
}

// handlhandleSettingseWallets handles the 'settings' page request.
func (s *webServer) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.sendTemplate(w, r, "settings", commonArgs(r, "Settings | Decred DEX"))
}

// apiRegister handles the 'register' page request.
func (s *webServer) apiRegister(w http.ResponseWriter, r *http.Request) {
	reg := new(core.Registration)
	if !readPost(w, r, reg) {
		return
	}
	var resp interface{}
	err := s.core.Register(reg)
	if err != nil {
		resp = map[string]interface{}{
			"ok":  false,
			"msg": fmt.Sprintf("registration error: %v", err),
		}
	} else {
		resp = map[string]interface{}{
			"ok": true,
		}
	}
	writeJSON(w, resp, s.indent)
}

// The loginForm is sent by the client to log in to a DEX.
type loginForm struct {
	DEX  string `json:"dex"`
	Pass string `json:"pass"`
}

// apiLogin handles the 'login' page request.
func (s *webServer) apiLogin(w http.ResponseWriter, r *http.Request) {
	login := new(loginForm)
	if !readPost(w, r, login) {
		return
	}
	var resp interface{}
	err := s.core.Login(login.DEX, login.Pass)
	if err != nil {
		resp = map[string]interface{}{
			"ok":  false,
			"msg": fmt.Sprintf("login error: %v", err),
		}
	} else {
		ai, found := r.Context().Value(authCV).(*userInfo)
		if !found || !ai.authed {
			cval := s.auth()
			http.SetCookie(w, &http.Cookie{
				Name:  authCK,
				Path:  "/",
				Value: cval,
			})
		}
		resp = map[string]interface{}{
			"ok": true,
		}
	}
	writeJSON(w, resp, s.indent)
}

// decodeMarket decodes the market string into its two tickers.
func decodeMarket(mkt string) (string, string, error) {
	mkts := strings.Split(mkt, "-")
	if len(mkts) != 2 {
		return "", "", fmt.Errorf("unable to decode markets from %s", mkt)
	}
	return mkts[0], mkts[1], nil
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
// for template building.
type userInfo struct {
	authed   bool
	DarkMode bool
}

// extract the userInfo from the request context.
func extractUserInfo(r *http.Request) *userInfo {
	ai, ok := r.Context().Value(authCV).(*userInfo)
	if !ok {
		log.Errorf("no auth info retrieved from client")
		return &userInfo{}
	}
	return ai
}

// authMiddleware checks incoming requests for authentication cookie-based
// information including the auth token.
func (s *webServer) authMiddleware(next http.Handler) http.Handler {
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

// writeJSON writes marshals the provided interface and writes the bytes to the
// ResponseWriter. The response code is assumed to be StatusOK.
func writeJSON(w http.ResponseWriter, thing interface{}, indent bool) {
	writeJSONWithStatus(w, thing, http.StatusOK, indent)
}

// writeJSON writes marshals the provided interface and writes the bytes to the
// ResponseWriter with the provided response code.
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
