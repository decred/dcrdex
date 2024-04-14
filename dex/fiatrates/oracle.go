package fiatrates

import (
	"context"
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
	averageRateRefreshInterval = defaultRefreshInterval + 1*time.Minute
)

// Oracle manages and retrieves fiat rate information from all enabled rate
// sources.
type Oracle struct {
	sources   []*source
	ratesMtx  sync.RWMutex
	fiatRates map[string]*FiatRateInfo
}

func NewFiatOracle(cfg Config) *Oracle {
	return &Oracle{
		fiatRates: make(map[string]*FiatRateInfo),
		sources:   fiatSources(cfg),
	}
}

// assets retrieves all assets that data can be fetched for.
func (o *Oracle) assets() []string {
	o.ratesMtx.Lock()
	defer o.ratesMtx.Unlock()
	var assets []string
	for sym := range o.fiatRates {
		assets = append(assets, sym)
	}
	return assets
}

// Rate returns the current fiat rate information for the provided symbol.
// Returns zero if there are no rates for the provided symbol yet.
func (o *Oracle) Rate(symbol string) float64 {
	o.ratesMtx.Lock()
	defer o.ratesMtx.Unlock()

	symbol = parseSymbol(symbol)
	rateInfo := o.fiatRates[symbol]
	hasRateInfo := rateInfo != nil
	if hasRateInfo && time.Since(rateInfo.LastUpdate) < FiatRateDataExpiry && rateInfo.Rate > 0 {
		return rateInfo.Rate
	}

	if !hasRateInfo {
		// Initiate an entry for this asset. Data for this asset will be fetched
		// in the next refresh cycle.
		o.fiatRates[symbol] = new(FiatRateInfo)
	}

	return 0
}

// Run starts goroutines that refreshes fiat rate every source.refreshInterval.
// This should be called in a goroutine as it's blocking.
func (o *Oracle) Run(ctx context.Context, log dex.Logger) {
	var wg sync.WaitGroup
	var sourcesEnabled int
	for i := range o.sources {
		fiatSource := o.sources[i]
		if fiatSource.disabled {
			log.Infof("Fiat rate source %q is disabled...", fiatSource.name)
			continue
		}

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
					if len(o.fiatRates) == 0 || s.disabled { // nothing to fetch.
						continue
					}

					if len(s.rates) > 0 && time.Since(s.lastRefresh) > FiatRateDataExpiry {
						s.mtx.Lock()
						s.rates = nil // empty previous rates
						s.disabled = true
						s.mtx.Unlock()
						log.Errorf("Fiat rate source %q has been disabled due to lack of fresh data...", s.name)
						return
					}

					assets := o.assets()
					if len(assets) == 0 {
						continue // nothing to do
					}

					newRates, err := s.getRates(ctx, assets, log)
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
		}(fiatSource)

		sourcesEnabled++
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
					if len(o.fiatRates) == 0 {
						continue // nothing to do
					}

					newRatesInfo := make(map[string]*fiatRateAndSourceCount)
					for i := range o.sources {
						s := o.sources[i]
						if s.disabled {
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
					for sym := range o.fiatRates {
						rateInfo := newRatesInfo[sym]
						if rateInfo != nil {
							o.fiatRates[sym].Rate = rateInfo.totalFiatRate / float64(rateInfo.sources)
							o.fiatRates[sym].LastUpdate = now
						}
					}
					o.ratesMtx.Unlock()
				}
			}
		}()
	}

	wg.Wait()
}
