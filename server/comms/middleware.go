// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package comms

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"github.com/go-chi/chi"
)

type contextKey int

// These are the keys for different types of values stored in a request context.
const (
	ctxThing contextKey = iota
)

// limitRate is rate-limiting middleware that checks whether a request can be
// fulfilled.
func (s *Server) limitRate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code, err := s.meterIP(dex.NewIPKey(r.RemoteAddr))
		if err != nil {
			http.Error(w, err.Error(), code)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// meterIP checks the dataEnabled flag, the global rate limiter, and the
// more restrictive ip-based rate limiter.
func (s *Server) meterIP(ip dex.IPKey) (int, error) {
	if atomic.LoadUint32(&s.dataEnabled) != 1 {
		return http.StatusServiceUnavailable, fmt.Errorf("data API is disabled")
	}
	if !globalHTTPRateLimiter.Allow() {
		return http.StatusTooManyRequests, fmt.Errorf("too many global requests")
	}
	ipLimiter := getIPLimiter(ip)
	if !ipLimiter.Allow() {
		return http.StatusTooManyRequests, fmt.Errorf(http.StatusText(http.StatusTooManyRequests))
	}
	return 0, nil
}

// candlesParamsParser is middleware for the /candles routes. Parses the
// *msgjson.CandlesRequest from the URL parameters.
func candleParamsParser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		baseID, quoteID, errMsg := parseBaseQuoteIDs(r)
		if errMsg != "" {
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}

		// Ensure the bin size is a valid duration string.
		binSize := chi.URLParam(r, "binSize")
		_, err := time.ParseDuration(binSize)
		if err != nil {
			http.Error(w, "bin size unparseable", http.StatusBadRequest)
			return
		}

		countStr := chi.URLParam(r, "count")
		count := 0
		if countStr != "" {
			count, err = strconv.Atoi(countStr)
			if err != nil {
				http.Error(w, "count unparseable", http.StatusBadRequest)
				return
			}
		}
		ctx := context.WithValue(r.Context(), ctxThing, &msgjson.CandlesRequest{
			BaseID:     baseID,
			QuoteID:    quoteID,
			BinSize:    binSize,
			NumCandles: count,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// orderBookParamsParser is middleware for the /orderbook route. Parses the
// *msgjson.OrderBookSubscription from the URL parameters.
func orderBookParamsParser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		baseID, quoteID, errMsg := parseBaseQuoteIDs(r)
		if errMsg != "" {
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
		ctx := context.WithValue(r.Context(), ctxThing, &msgjson.OrderBookSubscription{
			Base:  baseID,
			Quote: quoteID,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// parseBaseQuoteIDs parses the "baseSymbol" and "quoteSymbol" URL parameters
// from the request.
func parseBaseQuoteIDs(r *http.Request) (baseID, quoteID uint32, errMsg string) {
	baseID, found := dex.BipSymbolID(chi.URLParam(r, "baseSymbol"))
	if !found {
		return 0, 0, "unknown base"
	}
	quoteID, found = dex.BipSymbolID(chi.URLParam(r, "quoteSymbol"))
	if !found {
		return 0, 0, "unknown quote"
	}
	return
}
