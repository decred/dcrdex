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

// handleHome is the handler for the '/' page request. The response depends on
// whether the user is authenticated or not.
func (s *WebServer) handleHome(w http.ResponseWriter, r *http.Request) {
	user := extractUserInfo(r)
	switch {
	// The registration page also walks the user through setting up their app
	// password and connecting the Decred wallet, if not already done.
	case !user.Initialized || (user.Authed && len(user.Markets) == 0):
		s.handleRegister(w, r)
		return
	case !user.Authed:
		s.handleLogin(w, r)
		return
	default:
		s.handleMarkets(w, r)
		return
	}
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
	user := extractUserInfo(r)
	if !user.Initialized {
		s.handleRegister(w, r)
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
	dcrID, _ := dex.BipSymbolID("dcr")
	status := s.core.WalletState(dcrID)
	exists := status != nil
	open := exists && status.Open
	needsInit := !user.Initialized
	data := &registerTmplData{
		CommonArguments: *commonArgs(r, "Register | Decred DEX"),
		InitStep:        needsInit,
		WalletStep:      !needsInit && !exists,
		OpenStep:        exists && !open,
		DEXStep:         exists && open,
	}
	s.sendTemplate(w, "register", data)
}

// marketResult is the template data for the `/markets` page request.
type marketTmplData struct {
	CommonArguments
	Markets map[string][]*core.Market
}

// marketResult returns a *marketResult, which is needed to process the
// 'markets' template.
func (s *WebServer) marketResult(r *http.Request) *marketTmplData {
	return &marketTmplData{
		CommonArguments: *commonArgs(r, "Markets | Decred DEX"),
		Markets:         s.core.Markets(),
	}
}

// handleMarkets is the handler for the '/markets' page request.
func (s *WebServer) handleMarkets(w http.ResponseWriter, r *http.Request) {
	s.sendTemplate(w, "markets", s.marketResult(r))
}

type walletsTmplData struct {
	CommonArguments
	Assets []*core.SupportedAsset
}

// handleWallets is the handler for the '/wallets' page request.
func (s *WebServer) handleWallets(w http.ResponseWriter, r *http.Request) {
	user := extractUserInfo(r)
	switch {
	// The registration page also walks the user through setting up their app
	// password and connecting the Decred wallet, if not already done.
	case !user.Initialized:
		s.handleRegister(w, r)
		return
	case !user.Authed:
		s.handleLogin(w, r)
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
