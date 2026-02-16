// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
	"github.com/go-chi/chi/v5"
)

type ctxID int

const (
	ctxOID ctxID = iota
	ctxHost
	ctxProposalToken
)

// securityMiddleware adds security headers to the server responses.
func (s *WebServer) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-frame-options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", s.csp)
		w.Header().Set("Permissions-Policy", "geolocation=(), midi=(), sync-xhr=(self), microphone=(), camera=(), magnetometer=(), gyroscope=(), fullscreen=(self), payment=()")
		next.ServeHTTP(w, r)
	})
}

// authMiddleware checks incoming requests for cookie-based information
// including the auth token. Use extractUserInfo to access the *userInfo in
// downstream handlers. This should be used with care since it involves a call
// to (*Core).User, which can be expensive.
func (s *WebServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxKeyUserInfo, &userInfo{
			Authed:           s.isAuthed(r),
			PasswordIsCached: s.isPasswordCached(r),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// tokenAuthMiddleware checks for an dexauth query parameter.
// If present, it sets the dexauth cookie.  This allows for passwordless
// login in the context of mobile companion apps.
func (s *WebServer) tokenAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries := r.URL.Query()
		authToken := queries.Get(authCK)
		if authToken != "" {
			setCookie(authCK, authToken, w)
		}
		next.ServeHTTP(w, r)
	})
}

// extractBooleanCookie extracts the cookie value with key k from the Request,
// and interprets the value as true only if it's equal to the string "1".
func extractBooleanCookie(r *http.Request, k string, defaultVal bool) bool {
	cookie, err := r.Cookie(k)
	switch {
	// Dark mode is the default
	case err == nil:
		return cookie.Value == "1"
	case errors.Is(err, http.ErrNoCookie):
	default:
		log.Errorf("Cookie %q retrieval error: %v", k, err)
	}
	return defaultVal
}

// requireInit ensures that the core app is initialized before allowing the
// incoming request to proceed. Redirects to the register page if the app is
// not initialized.
func (s *WebServer) requireInit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.core.IsInitialized() {
			http.Redirect(w, r, initRoute, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireNotInit ensures that the core app is not initialized before allowing
// the incoming request to proceed. Redirects to the login page if the app is
// initialized and the user is not logged in. If logged in, directs to the
// wallets page.
func (s *WebServer) requireNotInit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.core.IsInitialized() {
			route := loginRoute
			if extractUserInfo(r).Authed {
				route = walletsRoute
			}
			http.Redirect(w, r, route, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// rejectUninited is like requireInit except that it responds with an error
// instead of redirecting to the register path.
func (s *WebServer) rejectUninited(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.core.IsInitialized() {
			http.Error(w, http.StatusText(http.StatusPreconditionRequired), http.StatusPreconditionRequired)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireLogin ensures that the user is authenticated (has logged in) before
// allowing the incoming request to proceed. Redirects to login page if user is
// not logged in. This check should typically be performed after checking that
// the app is initialized.
func (s *WebServer) requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthed(r) {
			http.Redirect(w, r, loginRoute, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// rejectUnauthed is like requireLogin except that it responds with an error
// instead of redirecting to the login path.
func (s *WebServer) rejectUnauthed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.isAuthed(r) {
			http.Error(w, "not authorized - login first", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireDEXConnection ensures that the user has completely registered with at
// least 1 DEX before allowing the incoming request to proceed. Redirects to the
// register page if the user has not connected any DEX.
func (s *WebServer) requireDEXConnection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.core.Exchanges()) == 0 {
			http.Redirect(w, r, registerRoute, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// dexHostCtx embeds the host into the request context.
func dexHostCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := chi.URLParam(r, "host")
		ctx := context.WithValue(r.Context(), ctxHost, host)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// getHostCtx interprets the context value at ctxHost as a string host.
func getHostCtx(r *http.Request) (string, error) {
	untypedHost := r.Context().Value(ctxHost)
	if untypedHost == nil {
		return "", errors.New("host not set in request")
	}
	host, ok := untypedHost.(string)
	if !ok {
		return "", errors.New("type assertion failed")
	}
	return host, nil
}

// orderIDCtx embeds order ID into the request context.
func orderIDCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oid := chi.URLParam(r, "oid")
		ctx := context.WithValue(r.Context(), ctxOID, oid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// getOrderIDCtx interprets the context value at ctxOID as a dex.Bytes order ID.
func getOrderIDCtx(r *http.Request) (dex.Bytes, error) {
	untypedOID := r.Context().Value(ctxOID)
	if untypedOID == nil {
		log.Errorf("nil value for order ID context value")
	}
	hexID, ok := untypedOID.(string)
	if !ok {
		log.Errorf("getOrderIDCtx type assertion failed. Expected string, got %T", untypedOID)
		return nil, fmt.Errorf("type assertion failed")
	}

	if len(hexID) != order.OrderIDSize*2 {
		log.Errorf("getOrderIDCtx received order ID string of wrong length. wanted %d, got %d",
			order.OrderIDSize*2, len(hexID))
		return nil, fmt.Errorf("invalid order ID")
	}
	oidB, err := hex.DecodeString(hexID)
	if err != nil {
		log.Errorf("getOrderIDCtx received invalid hex for order ID %q", hexID)
		return nil, fmt.Errorf("")
	}
	return oidB, nil
}

// proposalTokenCtx embeds the proposal token into the request context.
func proposalTokenCtx(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := chi.URLParam(r, "token")
		ctx := context.WithValue(r.Context(), ctxProposalToken, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// getProposalTokenCtx interprets the context value at ctxProposalToken as a string.
func getProposalTokenCtx(r *http.Request) (string, error) {
	untypedToken := r.Context().Value(ctxProposalToken)
	if untypedToken == nil {
		return "", errors.New("proposal token not set in request")
	}
	token, ok := untypedToken.(string)
	if !ok {
		return "", errors.New("type assertion failed")
	}
	return token, nil
}
