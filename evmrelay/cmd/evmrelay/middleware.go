// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"decred.org/dcrdex/evmrelay"
)

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (s *relayServer) logHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
			r.Body.Close()
			if err != nil {
				r.Body = io.NopCloser(bytes.NewReader(nil))
			} else {
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			}
		}

		ip := clientIP(r, s.trustedProxies)
		log.Debugf("%s %s from %s", r.Method, r.URL.String(), ip)

		lw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next(lw, r)

		log.Debugf("%s %s -> %d: %s", r.Method, r.URL.String(), lw.status, strings.TrimSpace(lw.body.String()))
	}
}

func (s *relayServer) rateLimitHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.limiter.allow(clientIP(r, s.trustedProxies)) {
			writeJSON(w, http.StatusTooManyRequests, &evmrelay.ErrorResponse{Error: "rate limit exceeded"})
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Debugf("Error writing JSON response: %v", err)
	}
}

func hexDecode(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	return hex.DecodeString(s)
}
