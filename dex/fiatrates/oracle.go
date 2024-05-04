package fiatrates

import (
	"context"
	"errors"
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
	log               dex.Logger
	sources           []*source
	ratesMtx          sync.RWMutex
	rates             map[string]*FiatRateInfo
	rateBroadcastChan chan map[string]*FiatRateInfo
}

func NewFiatOracle(cfg Config, tickerSymbols string, log dex.Logger) (*Oracle, error) {
	fiatOracle := &Oracle{
		log:               log,
		rates:             make(map[string]*FiatRateInfo),
		sources:           fiatSources(cfg),
		rateBroadcastChan: make(chan map[string]*FiatRateInfo),
	}

	tickers := strings.Split(tickerSymbols, ",")
	for _, ticker := range tickers {
		_, ok := dex.BipSymbolID(strings.ToLower(ticker))
		if !ok {
			return nil, fmt.Errorf("unknown asset %s", ticker)
		}

		// Initialize entry for this asset.
		fiatOracle.rates[parseTicker(ticker)] = new(FiatRateInfo)
	}

	if len(fiatOracle.rates) == 0 {
		return nil, errors.New("a minimum of one ticker is expected to configure fiat oracle")
	}

	return fiatOracle, nil
}

// BroadcastChan returns a read only channel to listen for latest rates.
func (o *Oracle) BroadcastChan() <-chan map[string]*FiatRateInfo {
	return o.rateBroadcastChan
}

// tickers retrieves all tickers that data can be fetched for.
func (o *Oracle) tickers() []string {
	o.ratesMtx.RLock()
	defer o.ratesMtx.RUnlock()
	var tickers []string
	for ticker := range o.rates {
		tickers = append(tickers, ticker)
	}
	return tickers
}

// Run starts goroutines that refresh fiat rates every source.refreshInterval.
// This should be called in a goroutine as it's blocking.
func (o *Oracle) Run(ctx context.Context) {
	var wg sync.WaitGroup
	var sourcesEnabled int
	initializedWithTicker := o.hasTicker()
	for i := range o.sources {
		fiatSource := o.sources[i]
		if fiatSource.isDisabled() {
			o.log.Infof("Fiat rate source %q is disabled...", fiatSource.name)
			continue
		}

		o.fetchFromSource(ctx, fiatSource, &wg)
		sourcesEnabled++

		if !initializedWithTicker {
			continue
		}

		// Fetch rates now.
		newRates, err := fiatSource.getRates(ctx, o.tickers(), o.log)
		if err != nil {
			o.log.Errorf("failed to retrieve rate from %s: %v", fiatSource.name, err)
			continue
		}

		fiatSource.mtx.Lock()
		fiatSource.rates = newRates
		fiatSource.lastRefresh = time.Now()
		fiatSource.mtx.Unlock()
	}

	if initializedWithTicker {
		// Calculate average fiat rate now.
		o.calculateAverageRate(ctx, &wg)
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
					if !o.hasTicker() {
						continue // nothing to do
					}

					o.calculateAverageRate(ctx, &wg)
				}
			}
		}()
	}

	wg.Wait()
	close(o.rateBroadcastChan)
}

// calculateAverageRate is a shared function to support fiat average rate
// calculations before and after averageRateRefreshInterval.
func (o *Oracle) calculateAverageRate(ctx context.Context, wg *sync.WaitGroup) {
	newRatesInfo := make(map[string]*fiatRateAndSourceCount)
	for i := range o.sources {
		s := o.sources[i]
		if s.isDisabled() {
			if s.checkIfSourceCanReactivate() {
				// Start a new goroutine for this source.
				o.fetchFromSource(ctx, s, wg)
			}
			continue
		}

		s.mtx.RLock()
		sourceRates := s.rates
		s.mtx.RUnlock()

		for ticker, rate := range sourceRates {
			if rate == 0 {
				continue
			}

			info, ok := newRatesInfo[ticker]
			if !ok {
				info = new(fiatRateAndSourceCount)
				newRatesInfo[ticker] = info
			}

			info.sources++
			info.totalFiatRate += rate
		}
	}

	now := time.Now()
	broadcastRates := make(map[string]*FiatRateInfo)
	o.ratesMtx.Lock()
	for ticker := range o.rates {
		oldRate := o.rates[ticker].Value
		rateInfo := newRatesInfo[ticker]
		if rateInfo != nil {
			newRate := rateInfo.totalFiatRate / float64(rateInfo.sources)
			if oldRate != newRate && newRate > 0 {
				o.rates[ticker].Value = newRate
				o.rates[ticker].LastUpdate = now
				rate := *o.rates[ticker] // copy
				broadcastRates[ticker] = &rate
			}
		}
	}
	o.ratesMtx.Unlock()

	if len(broadcastRates) > 0 {
		o.rateBroadcastChan <- broadcastRates
	}
}

func (o *Oracle) hasTicker() bool {
	o.ratesMtx.RLock()
	defer o.ratesMtx.RUnlock()
	return len(o.rates) != 0
}

// fetchFromSource starts a goroutine that retrieves fiat rate from the provided
// source.
func (o *Oracle) fetchFromSource(ctx context.Context, s *source, wg *sync.WaitGroup) {
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
				if !o.hasTicker() || s.isDisabled() { // nothing to fetch.
					continue
				}

				if s.hasTicker() && s.isExpired() {
					s.deactivate()
					o.log.Errorf("Fiat rate source %q has been disabled due to lack of fresh data. It will be re-enabled after %d hours.", s.name, reactivateDuration.Hours())
					return
				}

				newRates, err := s.getRates(ctx, o.tickers(), o.log)
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
