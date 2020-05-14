// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"fmt"
	"net/http"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
)

// apiGetFee is the handler for the '/getfee' API request.
func (s *WebServer) apiGetFee(w http.ResponseWriter, r *http.Request) {
	form := new(registration)
	if !readPost(w, r, form) {
		return
	}
	fee, err := s.core.GetFee(form.URL, form.Cert)
	if err != nil {
		s.writeAPIError(w, "getfee error: %v", err)
		return
	}
	resp := struct {
		OK  bool   `json:"ok"`
		Fee uint64 `json:"fee,omitempty"`
	}{
		OK:  true,
		Fee: fee,
	}
	writeJSON(w, resp, s.indent)
}

// apiRegister is the handler for the '/register' API request.
func (s *WebServer) apiRegister(w http.ResponseWriter, r *http.Request) {
	reg := new(registration)
	defer reg.Password.Clear()
	if !readPost(w, r, reg) {
		return
	}
	dcrID, _ := dex.BipSymbolID("dcr")
	wallet := s.core.WalletState(dcrID)
	if wallet == nil {
		s.writeAPIError(w, "No Decred wallet")
		return
	}
	if !wallet.Running {
		s.writeAPIError(w, "Decred wallet not running")
		return
	}
	if !wallet.Open {
		s.writeAPIError(w, "Decred wallet is locked")
		return
	}

	err := s.core.Register(&core.RegisterForm{
		URL:     reg.URL,
		Cert:    reg.Cert,
		AppPass: reg.Password,
		Fee:     reg.Fee,
	})
	if err != nil {
		s.writeAPIError(w, "registration error: %v", err)
		return
	}
	// There was no error paying the fee, but we must wait on confirmations
	// before informing the DEX of the fee payment. Those results will come
	// through as a notification.
	writeJSON(w, simpleAck(), s.indent)
}

// apiNewWallet is the handler for the '/newwallet' API request.
func (s *WebServer) apiNewWallet(w http.ResponseWriter, r *http.Request) {
	form := new(newWalletForm)
	defer func() {
		form.AppPW.Clear()
		form.Pass.Clear()
	}()
	if !readPost(w, r, form) {
		return
	}
	has := s.core.WalletState(form.AssetID) != nil
	if has {
		s.writeAPIError(w, "already have a wallet for %s", unbip(form.AssetID))
		return
	}
	// Wallet does not exist yet. Try to create it.
	err := s.core.CreateWallet(form.AppPW, form.Pass, &core.WalletForm{
		AssetID:    form.AssetID,
		Account:    form.Account,
		ConfigText: form.Config,
	})
	if err != nil {
		s.writeAPIError(w, "error creating %s wallet: %v", unbip(form.AssetID), err)
		return
	}
	s.notifyWalletUpdate(form.AssetID)
	writeJSON(w, simpleAck(), s.indent)
}

// apiOpenWallet is the handler for the '/openwallet' API request. Unlocks the
// specified wallet.
func (s *WebServer) apiOpenWallet(w http.ResponseWriter, r *http.Request) {
	form := new(openWalletForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	status := s.core.WalletState(form.AssetID)
	if status == nil {
		s.writeAPIError(w, "No wallet for %d -> %s", form.AssetID, unbip(form.AssetID))
		return
	}
	err := s.core.OpenWallet(form.AssetID, form.Pass)
	if err != nil {
		s.writeAPIError(w, "error unlocking %s wallet: %v", unbip(form.AssetID), err)
		return
	}
	s.notifyWalletUpdate(form.AssetID)
	writeJSON(w, simpleAck(), s.indent)
}

// apiConnect is the handler for the '/connectwallet' API request. Connects to
// a specified wallet, but does not unlock it.
func (s *WebServer) apiConnect(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		AssetID uint32 `json:"assetID"`
	}{}
	err := s.core.ConnectWallet(form.AssetID)
	if err != nil {
		s.writeAPIError(w, "error connecting to %s wallet: %v", unbip(form.AssetID), err)
		return
	}
	s.notifyWalletUpdate(form.AssetID)
	writeJSON(w, simpleAck(), s.indent)
}

// apiTrade is the handler for the '/trade' API request.
func (s *WebServer) apiTrade(w http.ResponseWriter, r *http.Request) {
	form := new(tradeForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	ord, err := s.core.Trade(form.Pass, form.Order)
	if err != nil {
		s.writeAPIError(w, "error placing order: %v", err)
		return
	}
	resp := &struct {
		OK    bool        `json:"ok"`
		Order *core.Order `json:"order"`
	}{
		OK:    true,
		Order: ord,
	}
	writeJSON(w, resp, s.indent)
}

// apiCancel is the handler for the '/cancel' API request.
func (s *WebServer) apiCancel(w http.ResponseWriter, r *http.Request) {
	form := new(cancelForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	err := s.core.Cancel(form.Pass, form.OrderID)
	if err != nil {
		s.writeAPIError(w, "error cancelling order %s: %v", form.OrderID, err)
		return
	}
	writeJSON(w, simpleAck(), s.indent)
}

// apiCloseWallet is the handler for the '/closewallet' API request.
func (s *WebServer) apiCloseWallet(w http.ResponseWriter, r *http.Request) {
	form := &struct {
		AssetID uint32 `json:"assetID"`
	}{}
	if !readPost(w, r, form) {
		return
	}
	err := s.core.CloseWallet(form.AssetID)
	if err != nil {
		s.writeAPIError(w, "error locking %s wallet: %v", unbip(form.AssetID), err)
		return
	}
	s.notifyWalletUpdate(form.AssetID)
	writeJSON(w, simpleAck(), s.indent)
}

// apiInit is the handler for the '/init' API request.
func (s *WebServer) apiInit(w http.ResponseWriter, r *http.Request) {
	login := new(loginForm)
	defer login.Pass.Clear()
	if !readPost(w, r, login) {
		return
	}
	err := s.core.InitializeClient(login.Pass)
	if err != nil {
		s.writeAPIError(w, "initialization error: %v", err)
		return
	}
	s.actuallyLogin(w, r, login)
}

// apiLogin handles the 'login' API request.
func (s *WebServer) apiLogin(w http.ResponseWriter, r *http.Request) {
	login := new(loginForm)
	defer login.Pass.Clear()
	if !readPost(w, r, login) {
		return
	}
	s.actuallyLogin(w, r, login)
}

// apiWithdraw handles the 'withdraw' API request.
func (s *WebServer) apiWithdraw(w http.ResponseWriter, r *http.Request) {
	form := new(withdrawForm)
	defer form.Pass.Clear()
	if !readPost(w, r, form) {
		return
	}
	state := s.core.WalletState(form.AssetID)
	if state == nil {
		s.writeAPIError(w, "no wallet found for %s", unbip(form.AssetID))
		return
	}
	coin, err := s.core.Withdraw(form.Pass, form.AssetID, form.Value)
	if err != nil {
		s.writeAPIError(w, "withdraw error: %v", err)
		return
	}
	resp := struct {
		OK   bool   `json:"ok"`
		Coin string `json:"coin"`
	}{
		OK:   true,
		Coin: coin.String(),
	}
	writeJSON(w, resp, s.indent)
}

// apiActuallyLogin logs the user in.
func (s *WebServer) actuallyLogin(w http.ResponseWriter, r *http.Request, login *loginForm) {
	notes, err := s.core.Login(login.Pass)
	if err != nil {
		s.writeAPIError(w, "login error: %v", err)
		return
	}
	ai, found := r.Context().Value(authCV).(*userInfo)
	if !found || !ai.Authed {
		cval := s.auth()
		http.SetCookie(w, &http.Cookie{
			Name:  authCK,
			Value: cval,
			Path:  "/",
			// The client should only send the cookie with first-party requests.
			// Cross-site requests should not include the auth cookie.
			// https://tools.ietf.org/html/draft-ietf-httpbis-cookie-same-site-00#section-4.1.1
			SameSite: http.SameSiteStrictMode,
			// Secure: false, // while false we require SameSite set
		})
	}
	writeJSON(w, struct {
		OK    bool               `json:"ok"`
		Notes []*db.Notification `json:"notes"`
	}{
		OK:    true,
		Notes: notes,
	}, s.indent)
}

// apiUser handles the 'user' API request.
func (s *WebServer) apiUser(w http.ResponseWriter, r *http.Request) {
	userInfo := extractUserInfo(r)
	response := struct {
		*core.User
		OK bool `json:"ok"`
	}{
		User: userInfo.User,
		OK:   true,
	}
	writeJSON(w, response, s.indent)
}

// writeAPIError logs the formatted error and sends a standardResponse with the
// error message.
func (s *WebServer) writeAPIError(w http.ResponseWriter, format string, a ...interface{}) {
	errMsg := fmt.Sprintf(format, a...)
	log.Error(errMsg)
	resp := &standardResponse{
		OK:  false,
		Msg: errMsg,
	}
	writeJSON(w, resp, s.indent)
}
