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

const (
	homeRoute     = "/"
	registerRoute = "/register"
	loginRoute    = "/login"
	marketsRoute  = "/markets"
	walletsRoute  = "/wallets"
	settingsRoute = "/settings"
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

// handleHome is the handler for the '/' page request. It redirects the
// requester to the markets page.
func (s *WebServer) handleHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, marketsRoute, http.StatusSeeOther)
}

// handleLogin is the handler for the '/login' page request.
func (s *WebServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	cArgs := commonArgs(r, "Login | Decred DEX")
	if cArgs.UserInfo.Authed {
		http.Redirect(w, r, marketsRoute, http.StatusSeeOther)
		return
	}
	s.sendTemplate(w, "login", cArgs)
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
	cArgs := commonArgs(r, "Register | Decred DEX")
	if cArgs.UserInfo.Initialized && !cArgs.UserInfo.Authed {
		// Initialized app should login before using the Register page.
		http.Redirect(w, r, loginRoute, http.StatusSeeOther)
		return
	}

	data := &registerTmplData{
		CommonArguments: *cArgs,
	}

	feeAssetID, _ := dex.BipSymbolID("dcr")
	feeWalletStatus := s.core.WalletState(feeAssetID)
	feeWalletExists := feeWalletStatus != nil
	feeWalletOpen := feeWalletExists && feeWalletStatus.Open

	switch {
	case !cArgs.UserInfo.Initialized:
		data.InitStep = true
	case !feeWalletExists:
		data.WalletStep = true
	case !feeWalletOpen:
		data.OpenStep = true
	default:
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
	cArgs := commonArgs(r, "Markets | Decred DEX")
	s.sendTemplate(w, "markets", &marketTmplData{
		CommonArguments: *cArgs,
	})
}

type walletsTmplData struct {
	CommonArguments
	Assets []*core.SupportedAsset
}

// handleWallets is the handler for the '/wallets' page request.
func (s *WebServer) handleWallets(w http.ResponseWriter, r *http.Request) {
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
