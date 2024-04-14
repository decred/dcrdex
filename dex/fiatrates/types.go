package fiatrates

import (
	"context"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
)

type Config struct {
	CryptoCompareAPIKey string `long:"ccdataapikey" description:"This is your free API Key from cryptocompare.com."`
	EnableBinanceUS     bool   `long:"enablebinanceus" description:"Set to true, if running the tatanka mesh from a US based server."`
	DisabledFiatSources string `long:"disabledfiatsources" description:"A list of disabled sources separated by comma. See fiatrate/sources.go"`
}

type source struct {
	name            string
	mtx             sync.RWMutex
	rates           map[string]float64
	disabled        bool
	lastRefresh     time.Time
	requestInterval time.Duration
	getRates        func(ctx context.Context, assets []string, log dex.Logger) (map[string]float64, error)
}

type fiatRateAndSourceCount struct {
	sources       int
	totalFiatRate float64
}

// FiatRateInfo holds the fiat rate and the last update time for an
// asset.
type FiatRateInfo struct {
	Rate       float64
	LastUpdate time.Time
}
