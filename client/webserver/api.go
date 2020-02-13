// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"fmt"
	"net/http"

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
)

// standardResponse is a basic API response when no data needs to be returned.
type standardResponse struct {
	OK  bool   `json:"ok"`
	Msg string `json:"msg,omitempty"`
}

// simpleAck is a plain standardResponse with "ok" = true.
func simpleAck() *standardResponse {
	return &standardResponse{
		OK: true,
	}
}

// preRegisterForm is the information necessary to pre-register a DEX.
type preRegisterForm struct {
	DEX string `json:"dex"`
}

// apiPreRegister is the handler for the '/preregister' API request.
func (s *WebServer) apiPreRegister(w http.ResponseWriter, r *http.Request) {
	form := new(preRegisterForm)
	if !readPost(w, r, form) {
		return
	}
	fee, err := s.core.PreRegister(form.DEX)
	if err != nil {
		s.writeAPIError(w, "preregister error: %v", err)
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

// registration is used to register a new DEX account.
type registration struct {
	DEX      string `json:"dex"`
	Password string `json:"pass"`
	Fee      uint64 `json:"fee"`
}

// apiRegister is the handler for the '/register' API request.
func (s *WebServer) apiRegister(w http.ResponseWriter, r *http.Request) {
	reg := new(registration)
	if !readPost(w, r, reg) {
		return
	}
	dcrID, _ := dex.BipSymbolID("dcr")
	has, on, open := s.core.WalletStatus(dcrID)
	if !has {
		s.writeAPIError(w, "No Decred wallet")
		return
	}
	if !on {
		s.writeAPIError(w, "Decred wallet not running")
		return
	}
	if !open {
		s.writeAPIError(w, "Decred wallet is locked")
		return
	}

	err, payFeeErr := s.core.Register(&core.Registration{
		DEX:      reg.DEX,
		Password: reg.Password,
		Fee:      reg.Fee,
	})
	if err != nil {
		s.writeAPIError(w, "registration error: %v", err)
		return
	}

	// There was no error paying the fee, but we must wait on confirmations
	// before informing the DEX of the fee payment. Those results will come
	// through on the error channel returned by Register, so the caller should
	// monitor the channel and send the result to the user as a notification.
	go func() {
		feeErr := <-payFeeErr
		if feeErr != nil {
			log.Errorf("Fee payment error: %v", feeErr)
			s.notify(errorMsgRoute, fmt.Sprintf("Error encountered while notifying DEX of registration fee: %v", err))
		} else {
			s.notify(successMsgRoute, fmt.Sprintf("Registration complete. You may now trade at %s.", reg.DEX))
		}
	}()
	writeJSON(w, simpleAck(), s.indent)
}

// newWalletForm is information necessary to create a new wallet.
type newWalletForm struct {
	AssetID uint32 `json:"assetID"`
	// These are only used if the Decred wallet does not already exist. In that
	// case, these parameters will be used to create the wallet.
	Account string `json:"account"`
	INIPath string `json:"inipath"`
	Pass    string `json:"pass"`
}

// apiNewWallet is the handler for the '/newwallet' API request.
func (s *WebServer) apiNewWallet(w http.ResponseWriter, r *http.Request) {
	form := new(newWalletForm)
	if !readPost(w, r, form) {
		return
	}
	has, _, _ := s.core.WalletStatus(form.AssetID)
	if has {
		s.writeAPIError(w, "already have a wallet for %s", unbip(form.AssetID))
		return
	}
	// Wallet does not exist yet. Try to create it.
	err := s.core.CreateWallet(&core.WalletForm{
		AssetID: form.AssetID,
		Account: form.Account,
		INIPath: form.INIPath,
	})
	if err != nil {
		s.writeAPIError(w, "error creating %s wallet: %v", unbip(form.AssetID), err)
		return
	}
	s.notifyWalletUpdate(form.AssetID, true, false)
	type response struct {
		OK     bool   `json:"ok"`
		Locked bool   `json:"locked"`
		Msg    string `json:"msg"`
	}
	err = s.core.OpenWallet(form.AssetID, form.Pass)
	if err != nil {
		errMsg := fmt.Sprintf("wallet connected, but failed to open with provided password: %v", err)
		log.Errorf(errMsg)
		resp := &response{
			Locked: true,
			Msg:    errMsg,
		}
		writeJSON(w, resp, s.indent)
		return
	}
	s.notifyWalletUpdate(form.AssetID, true, true)
	writeJSON(w, simpleAck(), s.indent)
}

// openWalletForm is information necessary to open a wallet.
type openWalletForm struct {
	AssetID uint32 `json:"assetID"`
	Pass    string `json:"pass"`
}

// apiOpenWallet is the handler for the '/openwallet' API request.
func (s *WebServer) apiOpenWallet(w http.ResponseWriter, r *http.Request) {
	form := new(openWalletForm)
	if !readPost(w, r, form) {
		return
	}
	has, on, _ := s.core.WalletStatus(form.AssetID)
	if !has {
		s.writeAPIError(w, "No wallet for %d -> %s", form.AssetID, unbip(form.AssetID))
		return
	}
	if !on {
		s.writeAPIError(w, "%s wallet not connected", unbip(form.AssetID))
		return
	}
	err := s.core.OpenWallet(form.AssetID, form.Pass)
	if err != nil {
		s.writeAPIError(w, "error unlocking %s wallet: %v", unbip(form.AssetID), err)
		return
	}
	s.notifyWalletUpdate(form.AssetID, true, true)
	writeJSON(w, simpleAck(), s.indent)
}

// The loginForm is sent by the client to log in to a DEX.
type loginForm struct {
	Pass string `json:"pass"`
}

// apiOpenWallet is the handler for the '/init' API request.
func (s *WebServer) apiInit(w http.ResponseWriter, r *http.Request) {
	login := new(loginForm)
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
	if !readPost(w, r, login) {
		return
	}
	s.actuallyLogin(w, r, login)
}

// apiActuallyLogin logs the user in.
func (s *WebServer) actuallyLogin(w http.ResponseWriter, r *http.Request, login *loginForm) {
	_, err := s.core.Login(login.Pass)
	if err != nil {
		s.writeAPIError(w, "login error: %v", err)
		return
	}
	ai, found := r.Context().Value(authCV).(*userInfo)
	if !found || !ai.Authed {
		cval := s.auth()
		http.SetCookie(w, &http.Cookie{
			Name:  authCK,
			Path:  "/",
			Value: cval,
		})
	}
	writeJSON(w, simpleAck(), s.indent)
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
