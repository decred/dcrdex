package fiatrates

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
)

const (
	// FiatRateDataExpiry : Any data older than FiatRateDataExpiry will be
	// discarded.
	FiatRateDataExpiry = 60 * time.Minute

	// averageRateRefreshInterval is how long it'll take before a fresh fiat
	// average rate is calculated.
	averageRateRefreshInterval = defaultRefreshInterval + time.Minute
)

// Oracle manages and retrieves fiat rate information from all enabled rate
// sources.
type Oracle struct {
	sources  []*source
	ratesMtx sync.RWMutex
	rates    map[string]*FiatRateInfo
}

func NewFiatOracle(cfg Config) (*Oracle, error) {
	fiatOracle := &Oracle{
		rates:   make(map[string]*FiatRateInfo),
		sources: fiatSources(cfg),
	}

	assets := strings.Split(cfg.Assets, ",")
	for _, asset := range assets {
		_, ok := dex.BipSymbolID(strings.ToLower(asset))
		if !ok {
			return nil, fmt.Errorf("unknown asset %s", asset)
		}

		// Initialize entry for this asset.
		fiatOracle.rates[parseSymbol(asset)] = new(FiatRateInfo)
	}

	return fiatOracle, nil
}

// Rate returns the current fiat rate information for the provided symbol.
// Returns zero if there are no rates for the provided symbol yet.
func (o *Oracle) Rate(symbol string) float64 {
	o.ratesMtx.Lock()
	defer o.ratesMtx.Unlock()

	symbol = parseSymbol(symbol)
	rateInfo := o.rates[symbol]
	hasRateInfo := rateInfo != nil
	if hasRateInfo && time.Since(rateInfo.LastUpdate) < FiatRateDataExpiry && rateInfo.Rate > 0 {
		return rateInfo.Rate
	}

	if !hasRateInfo {
		// Initiate an entry for this asset. Data for this asset will be fetched
		// in the next refresh cycle.
		o.rates[symbol] = new(FiatRateInfo)
	}

	return 0
}

// assets retrieves all assets that data can be fetched for.
func (o *Oracle) assets() []string {
	o.ratesMtx.RLock()
	defer o.ratesMtx.RUnlock()
	var assets []string
	for sym := range o.rates {
		assets = append(assets, sym)
	}
	return assets
}

// Run starts goroutines that refresh fiat rates every source.refreshInterval.
// This should be called in a goroutine as it's blocking.
func (o *Oracle) Run(ctx context.Context, log dex.Logger) {
	var wg sync.WaitGroup
	var sourcesEnabled int
	initializedWithAssets := o.hasAssets()
	for i := range o.sources {
		fiatSource := o.sources[i]
		if fiatSource.isDisabled() {
			log.Infof("Fiat rate source %q is disabled...", fiatSource.name)
			continue
		}

		o.fetchFromSource(ctx, fiatSource, &wg, log)
		sourcesEnabled++

		if !initializedWithAssets {
			continue
		}

		// Fetch rates now.
		newRates, err := fiatSource.getRates(ctx, o.assets(), log)
		if err != nil {
			log.Errorf("failed to retrieve rate from %s: %v", fiatSource.name, err)
			continue
		}

		fiatSource.mtx.Lock()
		fiatSource.rates = newRates
		fiatSource.lastRefresh = time.Now()
		fiatSource.mtx.Unlock()
	}

	if initializedWithAssets {
		// Calculate average fiat rate now.
		o.calculateAverageRate(ctx, &wg, log)
	}

	if sourcesEnabled > 0 {
		// Start a goroutine to generate an average fiat rate based on fresh
		// data from all enabled sources. This is done every
		// averageRateRefreshInterval.
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(averageRateRefreshInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if !o.hasAssets() {
						continue // nothing to do
					}

					o.calculateAverageRate(ctx, &wg, log)
				}
			}
		}()
	}

	wg.Wait()
}

// calculateAverageRate is a shared function to support fiat average rate
// calculations before and after averageRateRefreshInterval.
func (o *Oracle) calculateAverageRate(ctx context.Context, wg *sync.WaitGroup, log dex.Logger) {
	newRatesInfo := make(map[string]*fiatRateAndSourceCount)
	for i := range o.sources {
		s := o.sources[i]
		if s.isDisabled() {
			if s.checkIfSourceCanReactivate() {
				// Start a new goroutine for this source.
				o.fetchFromSource(ctx, s, wg, log)
			}
			continue
		}

		s.mtx.RLock()
		sourceRates := s.rates
		s.mtx.RUnlock()

		for sym, rate := range sourceRates {
			if rate == 0 {
				continue
			}

			info, ok := newRatesInfo[sym]
			if !ok {
				info = new(fiatRateAndSourceCount)
				newRatesInfo[sym] = info
			}

			info.sources++
			info.totalFiatRate += rate
		}
	}

	now := time.Now()
	o.ratesMtx.Lock()
	for sym := range o.rates {
		rateInfo := newRatesInfo[sym]
		if rateInfo != nil {
			o.rates[sym].Rate = rateInfo.totalFiatRate / float64(rateInfo.sources)
			o.rates[sym].LastUpdate = now
		}
	}
	o.ratesMtx.Unlock()
}

func (o *Oracle) hasAssets() bool {
	o.ratesMtx.RLock()
	defer o.ratesMtx.RUnlock()
	return len(o.rates) != 0
}

// fetchFromSource starts a goroutine that retrieves fiat rate from the provided
// source.
func (o *Oracle) fetchFromSource(ctx context.Context, s *source, wg *sync.WaitGroup, log dex.Logger) {
	wg.Add(1)
	go func(s *source) {
		defer wg.Done()
		ticker := time.NewTicker(s.requestInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !o.hasAssets() || s.isDisabled() { // nothing to fetch.
					continue
				}

				if s.hasAssets() && s.isExpired() {
					s.deactivate()
					log.Errorf("Fiat rate source %q has been disabled due to lack of fresh data. It will be re-enabled after %d hours.", s.name, reactivateDuration.Hours())
					return
				}

				newRates, err := s.getRates(ctx, o.assets(), log)
				if err != nil {
					log.Errorf("%s.getRates error: %v", s.name, err)
					continue
				}

				s.mtx.Lock()
				s.rates = newRates
				s.lastRefresh = time.Now()
				s.mtx.Unlock()
			}
		}
	}(s)
}
