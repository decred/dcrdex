// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"context"
	"crypto/elliptic"
	crand "crypto/rand"
	"crypto/tls"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/client/mm"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/client/tor"
	"decred.org/dcrdex/client/webserver/locales"
	"decred.org/dcrdex/client/websocket"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/encrypt"
	"github.com/decred/dcrd/certgen"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/text/language"
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
	// authCK is the authorization token cookie key.
	authCK = "dexauth"
	// pwKeyCK is the cookie used to unencrypt the user's password.
	pwKeyCK = "sessionkey"
	// ctxKeyUserInfo is used in the authorization middleware for saving user
	// info in http request contexts.
	ctxKeyUserInfo = contextKey("userinfo")
	// notifyRoute is a route used for general notifications.
	notifyRoute = "notify"
	// The basis for content-security-policy. connect-src must be the final
	// directive so that it can be reliably supplemented on startup.
	baseCSP = "default-src 'none'; script-src 'self'; img-src 'self' data:; style-src 'self'; font-src 'self'; connect-src 'self'"
	// site is the common prefix for the site resources with respect to this
	// webserver package.
	site = "site"
)

var (
	// errNoCachedPW is returned when attempting to retrieve a cached password, but the
	// cookie that should contain the cached password is not populated.
	errNoCachedPW = errors.New("no cached password")
)

var (
	log   dex.Logger
	unbip = dex.BipIDSymbol

	//go:embed site/src/html/*.tmpl
	htmlTmplRes    embed.FS
	htmlTmplSub, _ = fs.Sub(htmlTmplRes, "site/src/html") // unrooted slash separated path as per io/fs.ValidPath

	//go:embed site/dist site/src/img site/src/font
	staticSiteRes embed.FS
)

// clientCore is satisfied by core.Core.
type clientCore interface {
	websocket.Core
	Network() dex.Network
	Exchanges() map[string]*core.Exchange
	Exchange(host string) (*core.Exchange, error)
	PostBond(form *core.PostBondForm) (*core.PostBondResult, error)
	RedeemPrepaidBond(appPW []byte, code []byte, host string, certI any) (tier uint64, err error)
	UpdateBondOptions(form *core.BondOptionsForm) error
	Login(pw []byte) error
	InitializeClient(pw []byte, seed *string) (string, error)
	AssetBalance(assetID uint32) (*core.WalletBalance, error)
	CreateWallet(appPW, walletPW []byte, form *core.WalletForm) error
	OpenWallet(assetID uint32, pw []byte) error
	RescanWallet(assetID uint32, force bool) error
	RecoverWallet(assetID uint32, appPW []byte, force bool) error
	CloseWallet(assetID uint32) error
	ConnectWallet(assetID uint32) error
	Wallets() []*core.WalletState
	WalletState(assetID uint32) *core.WalletState
	WalletSettings(uint32) (map[string]string, error)
	ReconfigureWallet([]byte, []byte, *core.WalletForm) error
	ToggleWalletStatus(assetID uint32, disable bool) error
	ChangeAppPass([]byte, []byte) error
	ResetAppPass(newPass []byte, seed string) error
	NewDepositAddress(assetID uint32) (string, error)
	AutoWalletConfig(assetID uint32, walletType string) (map[string]string, error)
	User() *core.User
	GetDEXConfig(dexAddr string, certI any) (*core.Exchange, error)
	AddDEX(appPW []byte, dexAddr string, certI any) error
	DiscoverAccount(dexAddr string, pass []byte, certI any) (*core.Exchange, bool, error)
	SupportedAssets() map[uint32]*core.SupportedAsset
	Send(pw []byte, assetID uint32, value uint64, address string, subtract bool) (asset.Coin, error)
	Trade(pw []byte, form *core.TradeForm) (*core.Order, error)
	TradeAsync(pw []byte, form *core.TradeForm) (*core.InFlightOrder, error)
	Cancel(oid dex.Bytes) error
	NotificationFeed() *core.NoteFeed
	Logout() error
	Orders(*core.OrderFilter) ([]*core.Order, error)
	Order(oid dex.Bytes) (*core.Order, error)
	MaxBuy(host string, base, quote uint32, rate uint64) (*core.MaxOrderEstimate, error)
	MaxSell(host string, base, quote uint32) (*core.MaxOrderEstimate, error)
	AccountExport(pw []byte, host string) (*core.Account, []*db.Bond, error)
	AccountImport(pw []byte, account *core.Account, bonds []*db.Bond) error
	ToggleAccountStatus(pw []byte, host string, disable bool) error
	IsInitialized() bool
	ExportSeed(pw []byte) (string, error)
	PreOrder(*core.TradeForm) (*core.OrderEstimate, error)
	WalletLogFilePath(assetID uint32) (string, error)
	BondsFeeBuffer(assetID uint32) (uint64, error)
	PreAccelerateOrder(oidB dex.Bytes) (*core.PreAccelerate, error)
	AccelerateOrder(pw []byte, oidB dex.Bytes, newFeeRate uint64) (string, error)
	AccelerationEstimate(oidB dex.Bytes, newFeeRate uint64) (uint64, error)
	UpdateCert(host string, cert []byte) error
	UpdateDEXHost(oldHost, newHost string, appPW []byte, certI any) (*core.Exchange, error)
	WalletRestorationInfo(pw []byte, assetID uint32) ([]*asset.WalletRestoration, error)
	ToggleRateSourceStatus(src string, disable bool) error
	FiatRateSources() map[string]bool
	EstimateSendTxFee(address string, assetID uint32, value uint64, subtract, maxWithdraw bool) (fee uint64, isValidAddress bool, err error)
	ValidateAddress(address string, assetID uint32) (bool, error)
	DeleteArchivedRecordsWithBackup(olderThan *time.Time, saveMatchesToFile, saveOrdersToFile bool) (string, int, error)
	WalletPeers(assetID uint32) ([]*asset.WalletPeer, error)
	AddWalletPeer(assetID uint32, addr string) error
	RemoveWalletPeer(assetID uint32, addr string) error
	Notifications(n int) (notes, pokes []*db.Notification, _ error)
	ApproveToken(appPW []byte, assetID uint32, dexAddr string, onConrim func()) (string, error)
	UnapproveToken(appPW []byte, assetID uint32, version uint32) (string, error)
	ApproveTokenFee(assetID uint32, version uint32, approval bool) (uint64, error)
	StakeStatus(assetID uint32) (*asset.TicketStakingStatus, error)
	SetVSP(assetID uint32, addr string) error
	PurchaseTickets(assetID uint32, pw []byte, n int) error
	SetVotingPreferences(assetID uint32, choices, tSpendPolicy, treasuryPolicy map[string]string) error
	ListVSPs(assetID uint32) ([]*asset.VotingServiceProvider, error)
	TicketPage(assetID uint32, scanStart int32, n, skipN int) ([]*asset.Ticket, error)
	TxHistory(assetID uint32, n int, refID *string, past bool) ([]*asset.WalletTransaction, error)
	FundsMixingStats(assetID uint32) (*asset.FundsMixingStats, error)
	ConfigureFundsMixer(appPW []byte, assetID uint32, enabled bool) error
	SetLanguage(string) error
	Language() string
	TakeAction(assetID uint32, actionID string, actionB json.RawMessage) error
	RedeemGeocode(appPW, code []byte, msg string) (dex.Bytes, uint64, error)
	ExtensionModeConfig() *core.ExtensionModeConfig
}

type MMCore interface {
	MarketReport(host string, base, quote uint32) (*mm.MarketReport, error)
	StartBot(mkt *mm.StartConfig, alternateConfigPath *string, pw []byte) (err error)
	StopBot(mkt *mm.MarketWithHost) error
	UpdateCEXConfig(updatedCfg *mm.CEXConfig) error
	CEXBalance(cexName string, assetID uint32) (*libxc.ExchangeBalance, error)
	UpdateBotConfig(updatedCfg *mm.BotConfig) error
	RemoveBotConfig(host string, baseID, quoteID uint32) error
	Status() *mm.Status
	ArchivedRuns() ([]*mm.MarketMakingRun, error)
	RunOverview(startTime int64, mkt *mm.MarketWithHost) (*mm.MarketMakingRunOverview, error)
	RunLogs(startTime int64, mkt *mm.MarketWithHost, n uint64, refID *uint64, filter *mm.RunLogFilters) (events, updatedEvents []*mm.MarketMakingEvent, overview *mm.MarketMakingRunOverview, err error)
	CEXBook(host string, baseID, quoteID uint32) (buys, sells []*core.MiniOrder, _ error)
}

// genCertPair generates a key/cert pair to the paths provided.
func genCertPair(certFile, keyFile string, altDNSNames []string) error {
	log.Infof("Generating TLS certificates...")

	org := "dex webserver autogenerated cert"
	validUntil := time.Now().Add(390 * 24 * time.Hour) // https://blog.mozilla.org/security/2020/07/09/reducing-tls-certificate-lifespans-to-398-days/
	cert, key, err := certgen.NewTLSCertPair(elliptic.P384(), org,
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

	return nil
}

var _ clientCore = (*core.Core)(nil)

// cachedPassword consists of the serialized crypter and an encrypted password.
// A key stored in the cookies is used to deserialize the crypter, then the
// crypter is used to decrypt the password.
type cachedPassword struct {
	EncryptedPass     []byte
	SerializedCrypter []byte
}

type Config struct {
	DataDir       string
	Core          clientCore // *core.Core
	MarketMaker   MMCore     // *mm.MarketMaker
	Addr          string
	CustomSiteDir string
	Language      string
	Logger        dex.Logger
	UTC           bool // for stdout http request logging
	CertFile      string
	KeyFile       string
	// NoEmbed indicates to serve files from the system disk rather than the
	// embedded files. Since this is a developer setting, this also implies
	// reloading of templates on each request. Note that only embedded files
	// should be used by default since site files from older distributions may
	// be present on the disk. When NoEmbed is true, this also implies reloading
	// and execution of html templates on each request.
	NoEmbed         bool
	HttpProf        bool
	Tor             bool
	MainLogFilePath string
}

type valStamp struct {
	val   uint64
	stamp time.Time
}

// WebServer is a single-client http and websocket server enabling a browser
// interface to Bison Wallet.
type WebServer struct {
	ctx      context.Context
	dataDir  string
	wsServer *websocket.Server
	mux      *chi.Mux
	siteDir  string
	lang     atomic.Value // string
	langs    []string
	core     clientCore
	mm       MMCore
	addr     string
	csp      string
	srv      *http.Server
	html     atomic.Value // *templates
	tor      bool
	onion    string

	authMtx         sync.RWMutex
	authTokens      map[string]bool
	cachedPasswords map[string]*cachedPassword // cached passwords keyed by auth token

	bondBufMtx sync.Mutex
	bondBuf    map[uint32]valStamp

	useDEXBranding  bool
	mainLogFilePath string
}

// New is the constructor for a new WebServer. CustomSiteDir in the Config can
// be left blank, in which case a handful of default locations will be checked.
// This will work in most cases.
func New(cfg *Config) (*WebServer, error) {

	if cfg.Logger != nil {
		log = cfg.Logger
	}

	// Only look for files on disk if NoEmbed is set. This is necessary since
	// site files from older distributions may be present.
	var siteDir string // empty signals embedded files only
	if cfg.NoEmbed {
		// Look for the "site" folder in the executable's path, the working
		// directory, or relative to [repo root]/client/cmd/bisonw.
		execPath, err := os.Executable() // e.g. /usr/bin/bisonw
		if err != nil {
			return nil, fmt.Errorf("unable to locate executable path: %w", err)
		}
		execPath, err = filepath.EvalSymlinks(execPath) // e.g. /opt/decred/dex/bisonw
		if err != nil {
			return nil, fmt.Errorf("unable to locate executable path: %w", err)
		}
		execPath = filepath.Dir(execPath) // e.g. /opt/decred/dex

		absDir, _ := filepath.Abs(site)
		for _, dir := range []string{
			cfg.CustomSiteDir,
			filepath.Join(execPath, site),
			absDir,
			filepath.Clean(filepath.Join(execPath, "../../webserver/site")),
		} {
			if dir == "" {
				continue
			}
			log.Debugf("Looking for site in %s", dir)
			if folderExists(dir) {
				siteDir = dir
				break
			}
		}

		if siteDir == "" {
			return nil, fmt.Errorf("no HTML template files found. "+
				"Place the 'site' folder in the executable's directory %q or the working directory, "+
				"or run bisonw from within the client/cmd/bisonw source workspace folder, or specify the"+
				"'sitedir' configuration directive to bisonw.", execPath)
		}

		log.Infof("Located \"site\" folder at %v", siteDir)
	} else {
		// Developer should remember to rebuild the Go binary if they modify any
		// frontend files, otherwise they should run with --no-embed-site.
		log.Debugf("Using embedded site resources.")
	}

	// Create an HTTP router.
	mux := chi.NewRouter()
	httpServer := &http.Server{
		Handler:      mux,
		ReadTimeout:  httpConnTimeoutSeconds * time.Second, // slow requests should not hold connections opened
		WriteTimeout: 2 * time.Minute,                      // request to response time, must be long enough for slow handlers
	}

	if cfg.CertFile != "" || cfg.KeyFile != "" {
		// Find or create the key pair.
		keyExists := dex.FileExists(cfg.KeyFile)
		certExists := dex.FileExists(cfg.CertFile)
		if certExists != keyExists {
			return nil, fmt.Errorf("missing cert pair file")
		}
		if !keyExists {
			if err := genCertPair(cfg.CertFile, cfg.KeyFile, []string{cfg.Addr}); err != nil {
				return nil, err
			}
			// TODO: generate a separate CA certificate. Browsers don't like
			// that the site certificate is also a CA.
		}
		keyPair, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, err
		}
		log.Infof("Using HTTPS with certificate %v and key %v. "+
			"You may import the certificate as an authority (CA) in your browser, "+
			"or override the warning about a self-signed certificate. "+
			"Delete both files to regenerate them on next startup.",
			cfg.CertFile, cfg.KeyFile)
		httpServer.TLSConfig = &tls.Config{
			ServerName:   cfg.Addr,
			Certificates: []tls.Certificate{keyPair},
			MinVersion:   tls.VersionTLS12,
		}
		// Uncomment to disable HTTP/2:
		// httpServer.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler), 0)
	}

	lang := cfg.Core.Language()

	langs := make([]string, 0, len(localesMap))
	for l := range localesMap {
		langs = append(langs, l)
	}

	var useDEXBranding bool
	if xCfg := cfg.Core.ExtensionModeConfig(); xCfg != nil {
		useDEXBranding = xCfg.UseDEXBranding
	}

	// Make the server here so its methods can be registered.
	s := &WebServer{
		langs:           langs,
		core:            cfg.Core,
		mm:              cfg.MarketMaker,
		siteDir:         siteDir,
		mux:             mux,
		srv:             httpServer,
		addr:            cfg.Addr,
		dataDir:         cfg.DataDir,
		wsServer:        websocket.New(cfg.Core, log.SubLogger("WS")),
		authTokens:      make(map[string]bool),
		cachedPasswords: make(map[string]*cachedPassword),
		tor:             cfg.Tor,
		bondBuf:         map[uint32]valStamp{},
		useDEXBranding:  useDEXBranding,
		mainLogFilePath: cfg.MainLogFilePath,
	}
	s.lang.Store(lang)

	if err := s.buildTemplates(lang); err != nil {
		return nil, fmt.Errorf("error loading localized html templates: %v", err)
	}

	// Middleware
	mux.Use(middleware.RequestLogger(&middleware.DefaultLogFormatter{
		Logger: &chiLogger{ // logs with Trace()
			Logger: dex.StdOutLogger("MUX", log.Level(), cfg.UTC),
		},
		NoColor: runtime.GOOS == "windows",
	}))
	mux.Use(s.securityMiddleware)
	mux.Use(middleware.Recoverer)

	// Compress responses if using tor.
	if cfg.Tor {
		mux.Use(middleware.Compress(9))
	}

	// HTTP profiler
	if cfg.HttpProf {
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
		web.Use(s.tokenAuthMiddleware)
		// Inject user info for handlers that use extractUserInfo, which
		// includes most of the page handlers that use commonArgs to
		// inject the User object for page template execution.
		web.Use(s.authMiddleware)
		web.Get(settingsRoute, s.handleSettings)

		web.Get("/generateqrcode", s.handleGenerateQRCode)
		web.Get("/generatecompanionappqrcode", s.handleGenerateCompanionAppQRCode)

		web.Group(func(notInit chi.Router) {
			notInit.Use(s.requireNotInit)
			notInit.Get(initRoute, s.handleInit)
		})

		// The rest of the web handlers require initialization.
		web.Group(func(webInit chi.Router) {
			webInit.Use(s.requireInit)

			webInit.Route(registerRoute, func(rr chi.Router) {
				rr.Get("/", s.handleRegister)
				rr.With(dexHostCtx).Get("/{host}", s.handleRegister)
			})

			webInit.Group(func(webNoAuth chi.Router) {
				// The login handler requires init but not auth since
				// it performs the auth.
				webNoAuth.Get(loginRoute, s.handleLogin)

				// The rest of these handlers require both init and auth.
				webNoAuth.Group(func(webAuth chi.Router) {
					webAuth.Use(s.requireLogin)
					webAuth.Get(homeRoute, s.handleHome)
					webAuth.Get(walletsRoute, s.handleWallets)
					webAuth.Get(walletLogRoute, s.handleWalletLogFile)
				})
			})

			// Handlers requiring a DEX connection.
			webInit.Group(func(webDC chi.Router) {
				webDC.Use(s.requireDEXConnection, s.requireLogin)
				webDC.With(orderIDCtx).Get("/order/{oid}", s.handleOrder)
				webDC.Get(ordersRoute, s.handleOrders)
				webDC.Get(exportOrderRoute, s.handleExportOrders)
				webDC.Get(marketsRoute, s.handleMarkets)
				webDC.Get(mmSettingsRoute, s.handleMMSettings)
				webDC.Get(mmArchivesRoute, s.handleMMArchives)
				webDC.Get(mmLogsRoute, s.handleMMLogs)
				webDC.Get(marketMakerRoute, s.handleMarketMaking)
				webDC.With(dexHostCtx).Get("/dexsettings/{host}", s.handleDexSettings)
			})

		})
	})

	// api endpoints
	mux.Route("/api", func(r chi.Router) {
		r.Use(middleware.AllowContentType("application/json"))
		r.Post("/init", s.apiInit)
		r.Get("/isinitialized", s.apiIsInitialized)
		r.Post("/resetapppassword", s.apiResetAppPassword)
		r.Get("/user", s.apiUser)
		r.Post("/locale", s.apiLocale)
		r.Post("/setlocale", s.apiSetLocale)

		r.Group(func(apiInit chi.Router) {
			apiInit.Use(s.rejectUninited)
			apiInit.Post("/login", s.apiLogin)
			apiInit.Post("/getdexinfo", s.apiGetDEXInfo) // TODO: Seems unused.
			apiInit.Post("/adddex", s.apiAddDEX)
			apiInit.Post("/discoveracct", s.apiDiscoverAccount)
			apiInit.Post("/bondsfeebuffer", s.apiBondsFeeBuffer)
		})

		r.Group(func(apiAuth chi.Router) {
			apiAuth.Use(s.rejectUnauthed)
			apiAuth.Get("/notes", s.apiNotes)
			apiAuth.Post("/defaultwalletcfg", s.apiDefaultWalletCfg)
			apiAuth.Post("/postbond", s.apiPostBond)
			apiAuth.Post("/updatebondoptions", s.apiUpdateBondOptions)
			apiAuth.Post("/redeemprepaidbond", s.apiRedeemPrepaidBond)
			apiAuth.Post("/newwallet", s.apiNewWallet)
			apiAuth.Post("/openwallet", s.apiOpenWallet)
			apiAuth.Post("/depositaddress", s.apiNewDepositAddress)
			apiAuth.Post("/closewallet", s.apiCloseWallet)
			apiAuth.Post("/connectwallet", s.apiConnectWallet)
			apiAuth.Post("/rescanwallet", s.apiRescanWallet)
			apiAuth.Post("/recoverwallet", s.apiRecoverWallet)
			apiAuth.Post("/trade", s.apiTrade)
			apiAuth.Post("/tradeasync", s.apiTradeAsync)
			apiAuth.Post("/cancel", s.apiCancel)
			apiAuth.Post("/logout", s.apiLogout)
			apiAuth.Post("/balance", s.apiGetBalance)
			apiAuth.Post("/parseconfig", s.apiParseConfig)
			apiAuth.Post("/reconfigurewallet", s.apiReconfig)
			apiAuth.Post("/changeapppass", s.apiChangeAppPass)
			apiAuth.Post("/walletsettings", s.apiWalletSettings)
			apiAuth.Post("/togglewalletstatus", s.apiToggleWalletStatus)
			apiAuth.Post("/orders", s.apiOrders)
			apiAuth.Post("/order", s.apiOrder)
			apiAuth.Post("/send", s.apiSend)
			apiAuth.Post("/maxbuy", s.apiMaxBuy)
			apiAuth.Post("/maxsell", s.apiMaxSell)
			apiAuth.Post("/preorder", s.apiPreOrder)
			apiAuth.Post("/exportaccount", s.apiAccountExport)
			apiAuth.Post("/exportseed", s.apiExportSeed)
			apiAuth.Post("/importaccount", s.apiAccountImport)
			apiAuth.Post("/toggleaccountstatus", s.apiToggleAccountStatus)
			apiAuth.Post("/accelerateorder", s.apiAccelerateOrder)
			apiAuth.Post("/preaccelerate", s.apiPreAccelerate)
			apiAuth.Post("/accelerationestimate", s.apiAccelerationEstimate)
			apiAuth.Post("/updatecert", s.apiUpdateCert)
			apiAuth.Post("/updatedexhost", s.apiUpdateDEXHost)
			apiAuth.Post("/restorewalletinfo", s.apiRestoreWalletInfo)
			apiAuth.Post("/toggleratesource", s.apiToggleRateSource)
			apiAuth.Post("/validateaddress", s.apiValidateAddress)
			apiAuth.Post("/txfee", s.apiEstimateSendTxFee)
			apiAuth.Post("/deletearchivedrecords", s.apiDeleteArchivedRecords)
			apiAuth.Post("/getwalletpeers", s.apiGetWalletPeers)
			apiAuth.Post("/addwalletpeer", s.apiAddWalletPeer)
			apiAuth.Post("/removewalletpeer", s.apiRemoveWalletPeer)
			apiAuth.Post("/approvetoken", s.apiApproveToken)
			apiAuth.Post("/unapprovetoken", s.apiUnapproveToken)
			apiAuth.Post("/approvetokenfee", s.apiApproveTokenFee)
			apiAuth.Post("/txhistory", s.apiTxHistory)
			apiAuth.Post("/takeaction", s.apiTakeAction)
			apiAuth.Post("/redeemgamecode", s.redeemGameCode)
			apiAuth.Get("/exportapplog", s.apiExportAppLogs)

			apiAuth.Post("/stakestatus", s.apiStakeStatus)
			apiAuth.Post("/setvsp", s.apiSetVSP)
			apiAuth.Post("/purchasetickets", s.apiPurchaseTickets)
			apiAuth.Post("/setvotes", s.apiSetVotingPreferences)
			apiAuth.Post("/listvsps", s.apiListVSPs)
			apiAuth.Post("/ticketpage", s.apiTicketPage)

			apiAuth.Post("/mixingstats", s.apiMixingStats)
			apiAuth.Post("/configuremixer", s.apiConfigureMixer)

			apiAuth.Post("/startmarketmakingbot", s.apiStartMarketMakingBot)
			apiAuth.Post("/stopmarketmakingbot", s.apiStopMarketMakingBot)
			apiAuth.Post("/updatebotconfig", s.apiUpdateBotConfig)
			apiAuth.Post("/updatecexconfig", s.apiUpdateCEXConfig)
			apiAuth.Post("/removebotconfig", s.apiRemoveBotConfig)
			apiAuth.Get("/marketmakingstatus", s.apiMarketMakingStatus)
			apiAuth.Post("/marketreport", s.apiMarketReport)
			apiAuth.Post("/cexbalance", s.apiCEXBalance)
			apiAuth.Get("/archivedmmruns", s.apiArchivedRuns)
			apiAuth.Post("/mmrunlogs", s.apiRunLogs)
			apiAuth.Post("/cexbook", s.apiCEXBook)
		})
	})

	// Files
	fileServer(mux, "/js", siteDir, "dist", "text/javascript")
	fileServer(mux, "/css", siteDir, "dist", "text/css")
	fileServer(mux, "/img", siteDir, "src/img", "")
	fileServer(mux, "/font", siteDir, "src/font", "")

	return s, nil
}

// buildTemplates prepares the HTML templates, which are executed and served in
// sendTemplate. An empty siteDir indicates that the embedded templates in the
// htmlTmplSub FS should be used. If siteDir is set, the templates will be
// loaded from disk.
func (s *WebServer) buildTemplates(lang string) error {
	// Try to identify language.
	acceptLang, err := language.Parse(lang)
	if err != nil {
		return fmt.Errorf("unable to parse requested language: %v", err)
	}

	// Find acceptable match with available locales.
	langTags := make([]language.Tag, 0, len(locales.Locales))
	localeNames := make([]string, 0, len(locales.Locales))
	for localeName := range locales.Locales {
		lang, _ := language.Parse(localeName) // checked in init()
		langTags = append(langTags, lang)
		localeNames = append(localeNames, localeName)
	}
	_, idx, conf := language.NewMatcher(langTags).Match(acceptLang)
	localeName := localeNames[idx] // use index because tag may end up as something hyper specific like zh-Hans-u-rg-cnzzzz
	switch conf {
	case language.Exact, language.High, language.Low:
		log.Infof("Using language %v", localeName)
	case language.No:
		return fmt.Errorf("no match for %q in recognized languages %v", lang, localeNames)
	}

	var htmlDir string
	if s.siteDir == "" {
		log.Infof("Using embedded HTML templates")
	} else {
		htmlDir = filepath.Join(s.siteDir, "src", "html")
		log.Infof("Using HTML templates in %s", htmlDir)
	}

	bb := "bodybuilder"
	html := newTemplates(htmlDir, localeName).
		addTemplate("login", bb, "forms").
		addTemplate("register", bb, "forms").
		addTemplate("markets", bb, "forms").
		addTemplate("wallets", bb, "forms").
		addTemplate("settings", bb, "forms").
		addTemplate("orders", bb).
		addTemplate("order", bb, "forms").
		addTemplate("dexsettings", bb, "forms").
		addTemplate("init", bb).
		addTemplate("mm", bb, "forms").
		addTemplate("mmsettings", bb, "forms").
		addTemplate("mmarchives", bb).
		addTemplate("mmlogs", bb)
	s.html.Store(html)

	return html.buildErr()
}

// Addr gives the address on which WebServer is listening. Use only after
// Connect.
func (s *WebServer) Addr() string {
	return s.addr
}

// Connect starts the web server. Satisfies the dex.Connector interface.
func (s *WebServer) Connect(ctx context.Context) (*sync.WaitGroup, error) {
	var wg sync.WaitGroup
	listeners := make([]net.Listener, 0)
	var success bool

	if s.tor {
		if s.dataDir == "" {
			return nil, errors.New("tor enabled but no data directory was specified")
		}
		dataDir := filepath.Join(s.dataDir, "tor")
		svc, err := tor.New(dataDir, log.SubLogger("TOR"))
		if err != nil {
			return nil, fmt.Errorf("error intializing hidden service: %w", err)
		}
		cm := dex.NewConnectionMaster(svc)
		if err := cm.ConnectOnce(ctx); err != nil {
			return nil, fmt.Errorf("error connecting hidden service: %w", err)
		}
		defer func() {
			if !success {
				cm.Disconnect()
			}
		}()
		wg.Add(1)
		go func() {
			cm.Wait()
			wg.Done()
		}()
		s.onion = svc.OnionAddress()
		log.Infof("Hidden service address: %s", s.onion)

		listener, err := net.Listen("tcp4", svc.ServerAddress())
		if err != nil {
			return nil, fmt.Errorf("error generating listener for tor relay: %w", err)
		}
		listeners = append(listeners, listener)
	}

	// Start serving.
	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return nil, fmt.Errorf("Can't listen on %s. web server quitting: %w", s.addr, err)
	}
	https := s.srv.TLSConfig != nil
	if https {
		listener = tls.NewListener(listener, s.srv.TLSConfig)
	}
	listeners = append(listeners, listener)
	s.ctx = ctx

	addr, allowInCSP := prepareAddr(listener.Addr())
	if allowInCSP {
		// Work around a webkit (safari) bug with the handling of the
		// connect-src directive of content security policy. See:
		// https://bugs.webkit.org/show_bug.cgi?id=201591. TODO: Remove this
		// workaround since the issue has been fixed in newer versions of
		// Safari. When this is removed, the allowInCSP variable can be removed
		// but prepareAddr should still return 127.0.0.1 for unspecified
		// addresses.
		scheme := "ws"
		if s.srv.TLSConfig != nil {
			scheme = "wss"
		}
		s.csp = fmt.Sprintf("%s %s://%s", baseCSP, scheme, addr)
	}
	s.addr = addr

	// Shutdown the server on context cancellation.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		err := s.srv.Shutdown(context.Background())
		if err != nil {
			log.Errorf("Problem shutting down rpc: %v", err)
		}
		s.wsServer.Shutdown()
		log.Infof("Web server off")
	}()

	// Configure the websocket handler before starting the server.
	s.mux.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		s.wsServer.HandleConnect(ctx, w, r)
	})

	for _, listener := range listeners {
		wg.Add(1)
		go func(listener net.Listener) {
			log.Infof("Server listening on %s", listener.Addr())
			err := s.srv.Serve(listener)
			if !errors.Is(err, http.ErrServerClosed) {
				log.Warnf("unexpected (http.Server).Serve error: %v", err)
			}
			log.Debugf("RPC listener done for %s", listener.Addr())
			wg.Done()
		}(listener)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.readNotifications(ctx)
	}()

	log.Infof("Web server listening on %s (https = %v)", s.addr, https)
	scheme := "http"
	if https {
		scheme = "https"
	}
	fmt.Printf("\n\t****  OPEN IN YOUR BROWSER TO LOGIN AND TRADE  --->  %s://%s  ****\n\n",
		scheme, s.addr)

	success = true
	return &wg, nil
}

// prepareAddr prepares the listening address in case a :0 was provided.
func prepareAddr(addr net.Addr) (string, bool) {
	// If the IP is unspecified, default to `127.0.0.1`. This is a workaround
	// for an issue where all ip addresses other than exactly 127.0.0.1 will
	// always fail to match when used in CSP directives. See:
	// https://w3c.github.io/webappsec-csp/#match-hosts.
	defaultIP := net.IP{127, 0, 0, 1}
	tcpAddr, ok := addr.(*net.TCPAddr)
	if ok && (tcpAddr.IP.IsUnspecified() || tcpAddr.IP.Equal(defaultIP)) {
		return net.JoinHostPort(defaultIP.String(), strconv.Itoa(tcpAddr.Port)), true
	}

	return addr.String(), false
}

// authorize creates, stores, and returns a new auth token to identify the user.
// deauth should be used to invalidate tokens on logout.
func (s *WebServer) authorize() string {
	b := make([]byte, 32)
	crand.Read(b)
	token := hex.EncodeToString(b)
	zero(b)
	s.authMtx.Lock()
	s.authTokens[token] = true
	s.authMtx.Unlock()
	return token
}

// deauth invalidates all current auth tokens. All existing sessions will need
// to login again.
func (s *WebServer) deauth() {
	s.authMtx.Lock()
	s.authTokens = make(map[string]bool)
	s.cachedPasswords = make(map[string]*cachedPassword)
	s.authMtx.Unlock()
}

// getAuthToken checks the request for an auth token cookie and returns it.
// An empty string is returned if there is no auth token cookie.
func getAuthToken(r *http.Request) string {
	var authToken string
	cookie, err := r.Cookie(authCK)
	switch {
	case err == nil:
		authToken = cookie.Value
	case errors.Is(err, http.ErrNoCookie):
	default:
		log.Errorf("authToken retrieval error: %v", err)
	}

	return authToken
}

// getPWKey checks the request for a password key cookie. Returns an error
// if it does not exist or it is not valid.
func getPWKey(r *http.Request) ([]byte, error) {
	cookie, err := r.Cookie(pwKeyCK)
	switch {
	case err == nil:
		sessionKey, err := hex.DecodeString(cookie.Value)
		if err != nil {
			return nil, err
		}
		return sessionKey, nil
	case errors.Is(err, http.ErrNoCookie):
		return nil, nil
	default:
		return nil, err
	}
}

// isAuthed checks if the incoming request is from an authorized user/device.
// Requires the auth token cookie to be set in the request and for the token
// to match `WebServer.validAuthToken`.
func (s *WebServer) isAuthed(r *http.Request) bool {
	authToken := getAuthToken(r)
	if authToken == "" {
		return false
	}
	s.authMtx.RLock()
	defer s.authMtx.RUnlock()
	return s.authTokens[authToken]
}

// getCachedPassword retrieves the cached password for the user identified by authToken and
// presenting the specified key in their cookies.
func (s *WebServer) getCachedPassword(key []byte, authToken string) ([]byte, error) {
	s.authMtx.Lock()
	cachedPassword, ok := s.cachedPasswords[authToken]
	s.authMtx.Unlock()
	if !ok {
		return nil, fmt.Errorf("cached encrypted password not found for"+
			" auth token: %v", authToken)
	}

	crypter, err := encrypt.Deserialize(key, cachedPassword.SerializedCrypter)
	if err != nil {
		return nil, fmt.Errorf("error deserializing crypter: %w", err)
	}

	pw, err := crypter.Decrypt(cachedPassword.EncryptedPass)
	if err != nil {
		return nil, fmt.Errorf("error decrypting password: %w", err)
	}

	return pw, nil
}

// getCachedPasswordUsingRequest retrieves the cached password using the information
// in the request.
func (s *WebServer) getCachedPasswordUsingRequest(r *http.Request) ([]byte, error) {
	authToken := getAuthToken(r)
	if authToken == "" {
		return nil, errNoCachedPW
	}
	pwKeyBlob, err := getPWKey(r)
	if err != nil {
		return nil, err
	}
	if pwKeyBlob == nil {
		return nil, errNoCachedPW
	}
	return s.getCachedPassword(pwKeyBlob, authToken)
}

// cacheAppPassword encrypts the app password with a random encryption key and returns the key.
// The authToken is used to lookup the encrypted password when calling getCachedPassword.
func (s *WebServer) cacheAppPassword(appPW []byte, authToken string) ([]byte, error) {
	key := encode.RandomBytes(16)
	crypter := encrypt.NewCrypter(key)
	defer crypter.Close()
	encryptedPass, err := crypter.Encrypt(appPW)
	if err != nil {
		return nil, fmt.Errorf("error encrypting password: %v", err)
	}

	s.authMtx.Lock()
	s.cachedPasswords[authToken] = &cachedPassword{
		EncryptedPass:     encryptedPass,
		SerializedCrypter: crypter.Serialize(),
	}
	s.authMtx.Unlock()
	return key, nil
}

// isPasswordCached checks if a password can be retrieved from the encrypted
// password cache using the information in the request.
func (s *WebServer) isPasswordCached(r *http.Request) bool {
	_, err := s.getCachedPasswordUsingRequest(r)
	return err == nil
}

// readNotifications reads from the Core notification channel and relays to
// websocket clients.
func (s *WebServer) readNotifications(ctx context.Context) {
	ch := s.core.NotificationFeed()
	defer ch.ReturnFeed()

	for {
		select {
		case n := <-ch.C:
			s.wsServer.Notify(notifyRoute, n)
		case <-ctx.Done():
			return
		}
	}
}

// readPost unmarshals the request body into the provided interface.
func readPost(w http.ResponseWriter, r *http.Request, thing any) bool {
	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		log.Debugf("Error reading request body: %v", err)
		http.Error(w, "error reading JSON message", http.StatusBadRequest)
		return false
	}
	err = json.Unmarshal(body, thing)
	if err != nil {
		log.Debugf("failed to unmarshal JSON request: %v", err)
		log.Debugf("raw request: %s", string(body))
		http.Error(w, "failed to unmarshal JSON request", http.StatusBadRequest)
		return false
	}
	return true
}

// userInfo is information about the connected user. This type embeds the
// core.User type, adding fields specific to the users server authentication
// and cookies.
type userInfo struct {
	Authed           bool
	PasswordIsCached bool
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

func serveFile(w http.ResponseWriter, r *http.Request, fullFilePath string) {
	// Generate the full file system path and test for existence.
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

// fileServer is a file server for files in subDir with the parent folder
// siteDire. An empty siteDir means embedded files only are served. The
// pathPrefix is stripped from the request path when locating the file.
func fileServer(r chi.Router, pathPrefix, siteDir, subDir, forceContentType string) {
	if strings.ContainsAny(pathPrefix, "{}*") {
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
		upath = path.Clean(strings.TrimPrefix(upath, pathPrefix))

		// Deny directory listings (http.ServeFile recognizes index.html and
		// attempts to serve the directory contents instead).
		if strings.HasSuffix(upath, "/index.html") {
			http.NotFound(w, r)
			return
		}

		// On Windows, a common registry misconfiguration leads to
		// mime.TypeByExtension setting an incorrect type for .js files, causing
		// the browser to refuse to execute the JavaScript. The following
		// workaround may be removed when Go 1.19 becomes the minimum required
		// version: https://go-review.googlesource.com/c/go/+/406894
		// https://github.com/golang/go/issues/32350
		if forceContentType != "" {
			w.Header().Set("Content-Type", forceContentType)
		}

		// If siteDir is set, use system file system only.
		if siteDir != "" {
			fullFilePath := filepath.Join(siteDir, subDir, upath)
			serveFile(w, r, fullFilePath)
			return
		}

		// Use the embedded files only.
		fs := http.FS(staticSiteRes) // so f is an http.File instead of fs.File
		f, err := fs.Open(path.Join(site, subDir, upath))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		// return in case it is a directory
		stat, err := f.Stat()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if stat.IsDir() {
			http.NotFound(w, r)
			return
		}

		if forceContentType == "" {
			// http.ServeFile would do the following type detection.
			contentType := mime.TypeByExtension(filepath.Ext(upath))
			if contentType == "" {
				// Sniff out the content type. See http.serveContent.
				var buf [512]byte
				n, _ := io.ReadFull(f, buf[:])
				contentType = http.DetectContentType(buf[:n])
				_, err = f.Seek(0, io.SeekStart) // rewind to output whole file
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			if contentType != "" {
				w.Header().Set("Content-Type", contentType)
			} // else don't set it (plain)
		}

		sendSize := stat.Size()
		w.Header().Set("Content-Length", strconv.FormatInt(sendSize, 10))

		// TODO: Set Last-Modified for the embedded files.
		// if modTime != nil {
		// 	w.Header().Set("Last-Modified", modTime.Format(http.TimeFormat))
		// }

		_, err = io.CopyN(w, f, sendSize)
		if err != nil {
			log.Errorf("Writing response for path %q failed: %v", r.URL.Path, err)
			// Too late to write to header with error code.
		}
	}

	// For the chi.Mux, make sure a path that ends in "/" and append a "*".
	muxRoot := pathPrefix
	if pathPrefix != "/" && pathPrefix[len(pathPrefix)-1] != '/' {
		r.Get(pathPrefix, http.RedirectHandler(pathPrefix+"/", 301).ServeHTTP)
		muxRoot += "/"
	}
	muxRoot += "*"

	// Mount the http.HandlerFunc on the pathPrefix.
	r.Get(muxRoot, hf)
}

// writeJSON marshals the provided interface and writes the bytes to the
// ResponseWriter. The response code is assumed to be StatusOK.
func writeJSON(w http.ResponseWriter, thing any) {
	writeJSONWithStatus(w, thing, http.StatusOK)
}

// writeJSON writes marshals the provided interface and writes the bytes to the
// ResponseWriter with the specified response code.
func writeJSONWithStatus(w http.ResponseWriter, thing any, code int) {
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

func folderExists(fp string) bool {
	stat, err := os.Stat(fp)
	return err == nil && stat.IsDir()
}

// chiLogger is an adaptor around dex.Logger that satisfies
// chi/middleware.LoggerInterface for chi's DefaultLogFormatter.
type chiLogger struct {
	dex.Logger
}

func (l *chiLogger) Print(v ...any) {
	l.Trace(v...)
}
