package webserver

import (
	"context"
	"net/http"
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
		// darkMode cookie.
		var darkMode bool
		cookie, err := r.Cookie(darkModeCK)
		switch err {
		// Dark mode is the default
		case nil:
			darkMode = cookie.Value == "1"
		case http.ErrNoCookie:
			darkMode = true
		default:
			log.Errorf("Cookie dark mode retrieval error: %v", err)
		}

		ctx := context.WithValue(r.Context(), ctxKeyUserInfo, &userInfo{
			User:     s.core.User(),
			Authed:   s.isAuthed(r),
			DarkMode: darkMode,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *WebServer) requireInit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Uninitialized app must go to register.
		user := extractUserInfo(r)
		if !user.Initialized {
			http.Redirect(w, r, registerRoute, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *WebServer) requireLogin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Unauthenticated user must go to login.
		user := extractUserInfo(r)
		if !user.Authed {
			http.Redirect(w, r, loginRoute, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *WebServer) requireDEXConnection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// User with no connected DEX must go to register.
		user := extractUserInfo(r)
		if len(user.Exchanges) == 0 {
			http.Redirect(w, r, registerRoute, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
