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
	ctx      context.Context
	log      dex.Logger
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
		o.fetchFiatRateNow(symbol)
	}

	return 0
}

// fetchFiatRateNow attempts to retrieve fiat rate from enabled sources and
// creates an entry for the provided asset. It'll try until one source returns a
// non-zero value for the provided symbol. symbol must have been parsed by
// parseSymbol function. Must be called with a write lock held on o.ratesMtx.
func (o *Oracle) fetchFiatRateNow(symbol string) {
	var fiatRate float64
	defer func() {
		// Initiate an entry for this asset. Even if fiatRate is still zero when
		// we get here, data for this asset will be fetched in the next refresh
		// cycle.
		o.rates[symbol] = &FiatRateInfo{
			Rate:       fiatRate,
			LastUpdate: time.Now(),
		}
	}()

	for _, s := range o.sources {
		if s.isDisabled() {
			continue
		}

		newRate, err := s.getRates(o.ctx, []string{symbol}, o.log)
		if err != nil {
			o.log.Errorf("%s.getRates error: %v", s.name, err)
			continue
		}

		fiatRate = newRate[symbol]
		if fiatRate > 0 {
			break
		}
	}
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
	o.ctx = ctx
	o.log = log

	var wg sync.WaitGroup
	var sourcesEnabled int
	initializedWithAssets := o.hasAssets()
	for i := range o.sources {
		fiatSource := o.sources[i]
		if fiatSource.isDisabled() {
			o.log.Infof("Fiat rate source %q is disabled...", fiatSource.name)
			continue
		}

		o.fetchFromSource(fiatSource, &wg)
		sourcesEnabled++

		if !initializedWithAssets {
			continue
		}

		// Fetch rates now.
		newRates, err := fiatSource.getRates(o.ctx, o.assets(), o.log)
		if err != nil {
			o.log.Errorf("failed to retrieve rate from %s: %v", fiatSource.name, err)
			continue
		}

		fiatSource.mtx.Lock()
		fiatSource.rates = newRates
		fiatSource.lastRefresh = time.Now()
		fiatSource.mtx.Unlock()
	}

	if initializedWithAssets {
		// Calculate average fiat rate now.
		o.calculateAverageRate(&wg)
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
				case <-o.ctx.Done():
					return
				case <-ticker.C:
					if !o.hasAssets() {
						continue // nothing to do
					}

					o.calculateAverageRate(&wg)
				}
			}
		}()
	}

	wg.Wait()
}

// calculateAverageRate is a shared function to support fiat average rate
// calculations before and after averageRateRefreshInterval.
func (o *Oracle) calculateAverageRate(wg *sync.WaitGroup) {
	newRatesInfo := make(map[string]*fiatRateAndSourceCount)
	for i := range o.sources {
		s := o.sources[i]
		if s.isDisabled() {
			if s.checkIfSourceCanReactivate() {
				// Start a new goroutine for this source.
				o.fetchFromSource(s, wg)
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
func (o *Oracle) fetchFromSource(s *source, wg *sync.WaitGroup) {
	wg.Add(1)
	go func(s *source) {
		defer wg.Done()
		ticker := time.NewTicker(s.requestInterval)
		defer ticker.Stop()

		for {
			select {
			case <-o.ctx.Done():
				return
			case <-ticker.C:
				if !o.hasAssets() || s.isDisabled() { // nothing to fetch.
					continue
				}

				if s.hasAssets() && s.isExpired() {
					s.deactivate()
					o.log.Errorf("Fiat rate source %q has been disabled due to lack of fresh data. It will be re-enabled after %d hours.", s.name, reactivateDuration.Hours())
					return
				}

				newRates, err := s.getRates(o.ctx, o.assets(), o.log)
				if err != nil {
					o.log.Errorf("%s.getRates error: %v", s.name, err)
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
