package fiatrates

import (
	"context"
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
	Tickers             string `long:"tickers" description:"A list of comma separated tickers to fetch rates for when the oracle is activated. At least one ticker must be specified to activate fiat oracle."`
}

type source struct {
	name     string
	mtx      sync.RWMutex
	rates    map[string]float64
	disabled bool
	// canReactivate is set to false if source was disabled by admin.
	canReactivate   bool
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
