// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"fmt"
	"net/http"

	"decred.org/dcrdex/client/core"
)

// standardResponse is a basic API response when no data needs to be returned.
type standardResponse struct {
	OK  bool   `json:"ok"`
	Msg string `json:"msg,omitempty"`
}

// apiRegister is the handler for the '/register' page request.
func (s *WebServer) apiRegister(w http.ResponseWriter, r *http.Request) {
	reg := new(core.Registration)
	if !readPost(w, r, reg) {
		return
	}
	var resp interface{}
	err := s.core.Register(reg)
	if err != nil {
		resp = &standardResponse{
			OK:  false,
			Msg: fmt.Sprintf("registration error: %v", err),
		}
	} else {
		resp = &standardResponse{
			OK: true,
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
