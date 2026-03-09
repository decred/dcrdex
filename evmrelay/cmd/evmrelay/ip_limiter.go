// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	ipLimiterRate  = 10
	ipLimiterBurst = 20
)

// ipLimiterEntry tracks a per-IP rate limiter and when it was last used.
type ipLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// ipLimiter provides per-IP rate limiting.
type ipLimiter struct {
	sync.Mutex
	entries map[string]*ipLimiterEntry
}

func newIPLimiter() *ipLimiter {
	return &ipLimiter{entries: make(map[string]*ipLimiterEntry)}
}

// allow returns true if the request from the given IP is allowed.
func (l *ipLimiter) allow(ip string) bool {
	l.Lock()
	e, ok := l.entries[ip]
	if !ok {
		e = &ipLimiterEntry{limiter: rate.NewLimiter(ipLimiterRate, ipLimiterBurst)}
		l.entries[ip] = e
	}
	e.lastSeen = time.Now()
	l.Unlock()
	return e.limiter.Allow()
}

// prune removes entries that haven't been seen within maxAge.
func (l *ipLimiter) prune(maxAge time.Duration) {
	l.Lock()
	defer l.Unlock()
	now := time.Now()
	for ip, e := range l.entries {
		if now.Sub(e.lastSeen) > maxAge {
			delete(l.entries, ip)
		}
	}
}

// clientIP extracts the client IP address from the request. Proxy headers
// (X-Real-IP, X-Forwarded-For) are only honored when the direct connection
// comes from an address in trustedProxies. If trustedProxies is empty, proxy
// headers are ignored and RemoteAddr is always used.
func clientIP(r *http.Request, trustedProxies map[string]struct{}) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if len(trustedProxies) == 0 {
		return host
	}
	if _, ok := trustedProxies[host]; !ok {
		return host
	}
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		if parsed := net.ParseIP(strings.TrimSpace(ip)); parsed != nil {
			return parsed.String()
		}
	}
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		candidate := forwarded
		if i := strings.IndexByte(forwarded, ','); i > 0 {
			candidate = forwarded[:i]
		}
		if parsed := net.ParseIP(strings.TrimSpace(candidate)); parsed != nil {
			return parsed.String()
		}
	}
	return host
}
