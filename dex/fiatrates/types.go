package fiatrates

import (
	"context"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
)

// reactivateDuration is how long it'll take before an auto disabled rate source
// is auto re-enabled.
var reactivateDuration = 24*time.Hour + FiatRateDataExpiry

type Config struct {
	CryptoCompareAPIKey string `long:"ccdataapikey" description:"This is your free API Key from cryptocompare.com."`
	EnableBinanceUS     bool   `long:"enablebinanceus" description:"Set to true, if running the tatanka mesh from a US based server."`
	DisabledFiatSources string `long:"disabledfiatsources" description:"A list of disabled sources separated by comma. See fiatrate/sources.go."`
}

// AllFiatSourceDisabled checks if all currently supported fiat rate sources
// are disabled.
func (cfg Config) AllFiatSourceDisabled() bool {
	disabledSources := strings.ToLower(cfg.DisabledFiatSources)
	return strings.Contains(disabledSources, strings.ToLower(cryptoCompare)) && strings.Contains(disabledSources, strings.ToLower(binance)) &&
		strings.Contains(disabledSources, strings.ToLower(coinpaprika)) && strings.Contains(disabledSources, strings.ToLower(messari)) &&
		strings.Contains(disabledSources, strings.ToLower(kuCoin))
}

type source struct {
	name string
	mtx  sync.RWMutex
	// rates is a map of ticker to their fiat value. It will be empty if this
	// source is auto-deactivated.
	rates map[string]float64
	// disabled is set to true if this source is deactivated automatically or by
	// admin. Rates from this source would no longer be requested once it
	// disabled.
	disabled bool
	// canReactivate is set to false if source was disabled by admin.
	canReactivate bool
	// lastRefresh is the last time this source made a successful request for
	// ticker rates.
	lastRefresh     time.Time
	requestInterval time.Duration
	getRates        func(ctx context.Context, tickers []string, log dex.Logger) (map[string]float64, error)
}

func (s *source) isDisabled() bool {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return s.disabled
}

func (s *source) hasTicker() bool {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return len(s.rates) != 0
}

func (s *source) isExpired() bool {
	s.mtx.RLock()
	defer s.mtx.RUnlock()
	return time.Since(s.lastRefresh) > FiatRateDataExpiry
}

func (s *source) deactivate() {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.rates = nil // empty previous rates
	s.disabled = true
}

// checkIfSourceCanReactivate reactivates a disabled source
func (s *source) checkIfSourceCanReactivate() bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	if !s.disabled {
		return false
	}

	reactivate := s.canReactivate && !s.lastRefresh.IsZero() && time.Since(s.lastRefresh) > reactivateDuration
	s.disabled = reactivate
	return reactivate
}

type fiatRateAndSourceCount struct {
	sources       int
	totalFiatRate float64
}

// FiatRateInfo holds the fiat rate and the last update time for an
// asset.
type FiatRateInfo struct {
	Value      float64
	LastUpdate time.Time
}

func (f *FiatRateInfo) IsExpired() bool {
	return time.Since(f.LastUpdate) > FiatRateDataExpiry
}
