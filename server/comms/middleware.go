// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package comms

import (
	"fmt"
	"net/http"
	"sync/atomic"

	"decred.org/dcrdex/dex"
)

type contextKey int

// These are the keys for different types of values stored in a request context.
const (
	CtxThing contextKey = iota
	ctxListener
)

// LimitRate is rate-limiting middleware that checks whether a request can be
// fulfilled. This is intended for the /api HTTP endpoints.
func (s *Server) LimitRate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, err := s.meterIP(dex.NewIPKey(r.RemoteAddr))
		if err != nil {
			http.Error(w, err.Error(), code)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// meterIP applies the dataEnabled flag, the per-IP rate limiter, and the
// global HTTP rate limiter. This is only intended for the data API. The other
// websocket route handlers have different limiters that are shared between
// connections from the same IP, but not globally.
//
// The per-IP limiter is checked before the global limiter so that a single
// abusive source is rejected without consuming global rate limit tokens.
func (s *Server) meterIP(ip dex.IPKey) (int, error) {
	if atomic.LoadUint32(&s.dataEnabled) != 1 {
		return http.StatusServiceUnavailable, fmt.Errorf("data API is disabled")
	}
	ipLimiter := s.getIPLimiter(ip)
	if !ipLimiter.Allow() {
		return http.StatusTooManyRequests, fmt.Errorf("too many requests")
	}
	if !s.globalHTTPRateLimiter.Allow() {
		return http.StatusTooManyRequests, fmt.Errorf("too many global requests")
	}
	return 0, nil
}
