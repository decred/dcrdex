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

// registration is used to register a new DEX account.
type registration struct {
	DEX      string `json:"dex"`
	Password string `json:"dexpass"`
	// WalletPass will be ignored if the wallet is already open.
	WalletPass string `json:"walletpass"`
	// These are only used if the Decred wallet does not already exist. In that
	// case, these parameters will be used to create the wallet.
	Account string `json:"account"`
	INIPath string `json:"inipath"`
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
		// Wallet does not exist yet. Try to create it.
		err := s.core.CreateWallet(&core.WalletForm{
			AssetID: dcrID,
			Account: reg.Account,
			INIPath: reg.INIPath,
		})
		if err != nil {
			errMsg := fmt.Sprintf("error creating wallet: %v", err)
			log.Errorf(errMsg)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
	}
	walletUpdate := &core.WalletStatus{
		Symbol:  "dcr",
		AssetID: dcrID,
		Open:    open,
		Running: on,
	}
	s.notify(updateWalletRoute, walletUpdate)

	if !open {
		err := s.core.OpenWallet(dcrID, reg.WalletPass)
		if err != nil {
			errMsg := fmt.Sprintf("error opening wallet: %v", err)
			log.Error(errMsg)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
		walletUpdate.Open = true
		s.notify(updateWalletRoute, walletUpdate)
	}

	var resp interface{}
	err, payFeeErr := s.core.Register(&core.Registration{
		DEX:      reg.DEX,
		Password: reg.Password,
	})

	if err != nil {
		errMsg := fmt.Sprintf("registration error: %v", err)
		log.Error(errMsg)
		resp = &standardResponse{
			OK:  false,
			Msg: errMsg,
		}
	} else {
		resp = &standardResponse{
			OK: true,
		}
		// There was no error paying the fee, but we must wait on confirmations
		// before informing the DEX of the fee payment. Those results will come
		// through on the error channel returned by Register
		go func() {
			feeErr := <-payFeeErr
			if feeErr != nil {
				s.notify(errorMsgRoute, fmt.Sprintf("Error encountered while notifying DEX of registration fee: %v", err))
			} else {
				s.notify(successMsgRoute, fmt.Sprintf("Registration complete. You may now trade at %s.", reg.DEX))
			}
		}()
	}
	writeJSON(w, resp, s.indent)
}

// The loginForm is sent by the client to log in to a DEX.
type loginForm struct {
	DEX  string `json:"dex"`
	Pass string `json:"pass"`
}

// apiLogin handles the 'login' API request.
func (s *WebServer) apiLogin(w http.ResponseWriter, r *http.Request) {
	login := new(loginForm)
	if !readPost(w, r, login) {
		return
	}
	var resp interface{}
	err := s.core.Login(login.DEX, login.Pass)
	if err != nil {
		resp = &standardResponse{
			OK:  false,
			Msg: fmt.Sprintf("login error: %v", err),
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
		resp = &standardResponse{
			OK: true,
		}
	}
	writeJSON(w, resp, s.indent)
}

// apiUser handles the 'user' API request.
func (s *WebServer) apiUser(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.core.User(), s.indent)
}
