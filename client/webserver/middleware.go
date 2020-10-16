// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package webserver

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
	"github.com/go-chi/chi"
)

type ctxID int

const (
	ctxOID ctxID = iota
)

// securityMiddleware adds security headers to the server responses.
func securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-frame-options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; img-src 'self'; style-src 'self';  font-src 'self'; connect-src 'self'")
		w.Header().Set("Feature-Policy", "geolocation 'none'; midi 'none'; notifications 'none'; push 'none'; sync-xhr 'self'; microphone 'none'; camera 'none'; magnetometer 'none'; gyroscope 'none'; speaker 'none'; vibrate 'none'; fullscreen 'self'; payment 'none'")
		next.ServeHTTP(w, r)
	})
}

// authMiddleware checks incoming requests for cookie-based information
// including the auth token.
func (s *WebServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxKeyUserInfo, &userInfo{
			User:       s.core.User(),
			Authed:     s.isAuthed(r),
			DarkMode:   extractBooleanCookie(r, darkModeCK, true),
			ShowPopups: extractBooleanCookie(r, popupsCK, true),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractBooleanCookie extracts the cookie value with key k from the Request,
// and interprets the value as true only if it's equal to the string "1".
func extractBooleanCookie(r *http.Request, k string, defaultVal bool) bool {
	cookie, err := r.Cookie(k)
	switch err {
	// Dark mode is the default
	case nil:
		return cookie.Value == "1"
	case http.ErrNoCookie:
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
		user := extractUserInfo(r)
		if !user.Initialized {
			http.Redirect(w, r, registerRoute, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// rejectUninited is like requireInit except that it responds with an error
// instead of redirecting to the register path.
func (s *WebServer) rejectUninited(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := extractUserInfo(r)
		if !user.Initialized {
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
		user := extractUserInfo(r)
		if !user.Authed {
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
		user := extractUserInfo(r)
		if !user.Authed {
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
		user := extractUserInfo(r)
		if len(user.Exchanges) == 0 {
			http.Redirect(w, r, registerRoute, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// orderIDCtx embeds order ID into the request context
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
