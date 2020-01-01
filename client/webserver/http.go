// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"io"
	"net/http"

	"decred.org/dcrdex/client/core"
)

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

// handleHome is the handler for the '/' page request. The response depends on
// whether the user is authenticated or not.
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

// Create the CommonArguments for the request.
func commonArgs(r *http.Request, title string) *CommonArguments {
	return &CommonArguments{
		UserInfo: extractUserInfo(r),
		Title:    title,
	}
}

// handleLogin is the handler for the '/login' page request.
func (s *webServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	s.sendTemplate(w, r, "login", commonArgs(r, "Login | Decred DEX"))
}

// handleRegister is the handler for the '/register' page request.
func (s *webServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	s.sendTemplate(w, r, "register", commonArgs(r, "Register | Decred DEX"))
}

// marketResult is the template data for the `/markets` page request.
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

// handleMarkets is the handler for the '/markets' page request.
func (s *webServer) handleMarkets(w http.ResponseWriter, r *http.Request) {
	s.sendTemplate(w, r, "markets", s.marketResult(r))
}

// handleWallets is the handler for the '/wallets' page request.
func (s *webServer) handleWallets(w http.ResponseWriter, r *http.Request) {
	s.sendTemplate(w, r, "wallets", commonArgs(r, "Wallets | Decred DEX"))
}

// handleSettings is the handler for the '/settings' page request.
func (s *webServer) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.sendTemplate(w, r, "settings", commonArgs(r, "Settings | Decred DEX"))
}
