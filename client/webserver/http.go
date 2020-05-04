// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"io"
	"net/http"
	"sort"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
)

// sendTemplate processes the template and sends the result.
func (s *WebServer) sendTemplate(w http.ResponseWriter, tmplID string, data interface{}) {
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

// allow checks if the user should be allowed to visit the requested route.
// If not, a different (recommended) page handler is invoked to handle the
// request.
func (s *WebServer) allow(route string, w http.ResponseWriter, r *http.Request) bool {
	user := extractUserInfo(r)
	isLoginRoute := route == "/login"
	dexConnected := len(user.Exchanges) > 0

	// Once initialized, all pages except "/login" requires user to be logged in.
	if user.Initialized && !user.Authed {
		if !isLoginRoute {
			s.handleLogin(w, r)
		}
		return isLoginRoute
	}

	// Disallow visits to the markets page if the user has not connected a DEX,
	// redirect to the registration page instead. The registration page walks
	// the user through setting up their app password, connecting their Decred
	// wallet, connecting a DEX server and paying the server fee.
	if route == "/markets" && !dexConnected {
		s.handleRegister(w, r)
		return false
	}

	// Disallow visits to the login page if user is already logged in. Redirect
	// to the markets page instead.
	if isLoginRoute && user.Authed {
		s.handleMarkets(w, r)
		return false
	}

	return true
}

// handleHome is the handler for the '/' page request. The response depends on
// whether the user is authenticated or not and whether the user has connected
// a DEX server.
func (s *WebServer) handleHome(w http.ResponseWriter, r *http.Request) {
	if !s.allow("/", w, r) {
		return
	}
	s.handleMarkets(w, r)
}

// CommonArguments are common page arguments that must be supplied to every
// page to populate the <title> and <header> elements.
type CommonArguments struct {
	UserInfo *userInfo
	Title    string
}

// Create the CommonArguments for the request.
func commonArgs(r *http.Request, title string) *CommonArguments {
	return &CommonArguments{
		UserInfo: extractUserInfo(r),
		Title:    title,
	}
}

// handleLogin is the handler for the '/login' page request.
func (s *WebServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.allow("/login", w, r) {
		return
	}
	s.sendTemplate(w, "login", commonArgs(r, "Login | Decred DEX"))
}

// registerTmplData is template data for the /register page.
type registerTmplData struct {
	CommonArguments
	InitStep   bool
	WalletStep bool
	OpenStep   bool
	DEXStep    bool
}

// handleRegister is the handler for the '/register' page request.
func (s *WebServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	user := extractUserInfo(r)
	feeAssetID, _ := dex.BipSymbolID("dcr")
	feeWalletStatus := s.core.WalletState(feeAssetID)
	feeWalletExists := feeWalletStatus != nil
	feeWalletOpen := feeWalletExists && feeWalletStatus.Open

	data := &registerTmplData{
		CommonArguments: *commonArgs(r, "Register | Decred DEX"),
	}
	if !user.Initialized {
		data.InitStep = true
	} else if !feeWalletExists {
		data.WalletStep = true
	} else if !feeWalletOpen {
		data.OpenStep = true
	} else {
		data.DEXStep = true
	}

	s.sendTemplate(w, "register", data)
}

// marketResult is the template data for the `/markets` page request.
type marketTmplData struct {
	CommonArguments
	Exchanges map[string]*core.Exchange
}

// handleMarkets is the handler for the '/markets' page request.
func (s *WebServer) handleMarkets(w http.ResponseWriter, r *http.Request) {
	if !s.allow("/markets", w, r) {
		return
	}
	user := extractUserInfo(r)
	s.sendTemplate(w, "markets", &marketTmplData{
		CommonArguments: *commonArgs(r, "Markets | Decred DEX"),
		Exchanges:       user.Exchanges,
	})
}

type walletsTmplData struct {
	CommonArguments
	Assets []*core.SupportedAsset
}

// handleWallets is the handler for the '/wallets' page request.
func (s *WebServer) handleWallets(w http.ResponseWriter, r *http.Request) {
	if !s.allow("/wallets", w, r) {
		return
	}
	assetMap := s.core.SupportedAssets()
	// Sort assets by 1. wallet vs no wallet, and 2) alphabetically.
	assets := make([]*core.SupportedAsset, 0, len(assetMap))
	// over-allocating, but assuming user will not have set up most wallets.
	nowallets := make([]*core.SupportedAsset, 0, len(assetMap))
	for _, asset := range assetMap {
		if asset.Wallet == nil {
			nowallets = append(nowallets, asset)
		} else {
			assets = append(assets, asset)
		}
	}
	sort.Slice(assets, func(i, j int) bool {
		return assets[i].Info.Name < assets[j].Info.Name
	})
	sort.Slice(nowallets, func(i, j int) bool {
		return nowallets[i].Info.Name < nowallets[j].Info.Name
	})
	data := &walletsTmplData{
		CommonArguments: *commonArgs(r, "Wallets | Decred DEX"),
		Assets:          append(assets, nowallets...),
	}
	s.sendTemplate(w, "wallets", data)
}

// handleSettings is the handler for the '/settings' page request.
func (s *WebServer) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.sendTemplate(w, "settings", commonArgs(r, "Settings | Decred DEX"))
}
