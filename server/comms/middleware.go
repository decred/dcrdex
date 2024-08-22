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

// meterIP applies the dataEnabled flag, the global HTTP rate limiter, and the
// more restrictive IP-based rate limiter. This is only intended for the data
// API. The other websocket route handlers have different limiters that are
// shared between connections from the same IP, but not globally.
func (s *Server) meterIP(ip dex.IPKey) (int, error) {
	if atomic.LoadUint32(&s.dataEnabled) != 1 {
		return http.StatusServiceUnavailable, fmt.Errorf("data API is disabled")
	}
	if !globalHTTPRateLimiter.Allow() {
		return http.StatusTooManyRequests, fmt.Errorf("too many global requests")
	}
	ipLimiter := getIPLimiter(ip)
	if !ipLimiter.Allow() {
		return http.StatusTooManyRequests, fmt.Errorf("too many requests")
	}
	return 0, nil
}
