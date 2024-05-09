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
	log      dex.Logger
	sources  []*source
	ratesMtx sync.RWMutex
	rates    map[string]*FiatRateInfo

	listenersMtx sync.RWMutex
	listeners    map[string]chan<- map[string]*FiatRateInfo
}

func NewFiatOracle(cfg Config, tickerSymbols string, log dex.Logger) (*Oracle, error) {
	fiatOracle := &Oracle{
		log:       log,
		rates:     make(map[string]*FiatRateInfo),
		sources:   fiatSources(cfg),
		listeners: make(map[string]chan<- map[string]*FiatRateInfo),
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

// Rates returns the current fiat rates. Returns an empty map if there are no
// valid rates.
func (o *Oracle) Rates() map[string]*FiatRateInfo {
	o.ratesMtx.Lock()
	defer o.ratesMtx.Unlock()
	rates := make(map[string]*FiatRateInfo, len(o.rates))
	for ticker, rate := range o.rates {
		if rate.Value > 0 && !rate.IsExpired() {
			r := *rate
			rates[ticker] = &r
		}
	}
	return rates
}

// Run starts goroutines that refresh fiat rates every source.refreshInterval.
// This should be called in a goroutine as it's blocking.
func (o *Oracle) Run(ctx context.Context) {
	var wg sync.WaitGroup
	var sourcesEnabled int
	for i := range o.sources {
		fiatSource := o.sources[i]
		if fiatSource.isDisabled() {
			o.log.Infof("Fiat rate source %q is disabled...", fiatSource.name)
			continue
		}

		o.fetchFromSource(ctx, fiatSource, &wg)
		sourcesEnabled++

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

	// Calculate average fiat rate now.
	o.calculateAverageRate()

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
					reActivatedSources := o.calculateAverageRate()
					for _, index := range reActivatedSources {
						s := o.sources[index]
						// Start a new goroutine for this source.
						o.fetchFromSource(ctx, s, &wg)
					}
				}
			}
		}()
	}

	wg.Wait()

	o.listenersMtx.Lock()
	for id, rateChan := range o.listeners {
		close(rateChan) // we are done sending fiat rates
		delete(o.listeners, id)
	}
	o.listenersMtx.Unlock()
}

// AddFiatRateListener adds a new fiat rate listener for the provided uniqueID.
// Overrides existing rateChan if uniqueID already exists.
func (o *Oracle) AddFiatRateListener(uniqueID string, ratesChan chan<- map[string]*FiatRateInfo) {
	o.listenersMtx.Lock()
	defer o.listenersMtx.Unlock()
	o.listeners[uniqueID] = ratesChan
}

// RemoveFiatRateListener removes a fiat rate listener. no-op if there's no
// listener for the provided uniqueID. The fiat rate chan will be closed to
// signal to readers that we are done sending.
func (o *Oracle) RemoveFiatRateListener(uniqueID string) {
	o.listenersMtx.Lock()
	defer o.listenersMtx.Unlock()
	rateChan, ok := o.listeners[uniqueID]
	if !ok {
		return
	}

	delete(o.listeners, uniqueID)
	close(rateChan) // we are done sending.
}

// notifyListeners sends the provided rates to all listener.
func (o *Oracle) notifyListeners(rates map[string]*FiatRateInfo) {
	o.listenersMtx.RLock()
	defer o.listenersMtx.RUnlock()
	for _, rateChan := range o.listeners {
		rateChan <- rates
	}
}

// calculateAverageRate is a shared function to support fiat average rate
// calculations before and after averageRateRefreshInterval.
func (o *Oracle) calculateAverageRate() []int {
	var reActivatedSourceIndexes []int
	newRatesInfo := make(map[string]*fiatRateAndSourceCount)
	for i := range o.sources {
		s := o.sources[i]
		if s.isDisabled() {
			if s.checkIfSourceCanReactivate() {
				reActivatedSourceIndexes = append(reActivatedSourceIndexes, i)
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
		rateInfo := newRatesInfo[ticker]
		if rateInfo != nil {
			newRate := rateInfo.totalFiatRate / float64(rateInfo.sources)
			if newRate > 0 {
				o.rates[ticker].Value = newRate
				o.rates[ticker].LastUpdate = now
				rate := *o.rates[ticker] // copy
				broadcastRates[ticker] = &rate
			}
		}
	}
	o.ratesMtx.Unlock()

	if len(broadcastRates) > 0 {
		o.notifyListeners(broadcastRates)
	}

	return reActivatedSourceIndexes
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
				if s.isDisabled() { // nothing to fetch.
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
