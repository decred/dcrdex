// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
)

const (
	// DefaultFiatCurrency is the currency for displaying assets fiat value.
	DefaultFiatCurrency = "USD"
	// fiatRateRequestInterval is the amount of time between calls to the exchange API.
	fiatRateRequestInterval = 12 * time.Minute
	// fiatRateDataExpiry : Any data older than fiatRateDataExpiry will be discarded.
	fiatRateDataExpiry = 60 * time.Minute

	// Tokens. Used to identify fiat rate source, source name must not contain a
	// comma.
	messari       = "Messari"
	coinpaprika   = "Coinpaprika"
	dcrdataDotOrg = "dcrdata"
)

var (
	dcrDataURL     = "https://explorer.dcrdata.org/api/exchangerate"
	coinpaprikaURL = "https://api.coinpaprika.com/v1/tickers/%s"
	messariURL     = "https://data.messari.io/api/v1/assets/%s/metrics/market-data"
	btcBipID, _    = dex.BipSymbolID("btc")
	dcrBipID, _    = dex.BipSymbolID("dcr")
)

// fiatRateFetchers is the list of all supported fiat rate fetchers.
var fiatRateFetchers = map[string]rateFetcher{
	coinpaprika:   fetchCoinpaprikaRates,
	dcrdataDotOrg: fetchDcrdataRates,
	messari:       fetchMessariRates,
}

// fiatRateInfo holds the fiat rate and the last update time for an
// asset.
type fiatRateInfo struct {
	rate       float64
	lastUpdate time.Time
}

// rateFetcher can fetch fiat rates for assets from an API.
type rateFetcher func(context context.Context, logger dex.Logger, assets map[uint32]*SupportedAsset) map[uint32]float64

type commonRateSource struct {
	fetchRates rateFetcher

	mtx       sync.RWMutex
	fiatRates map[uint32]*fiatRateInfo
}

// isExpired checks the last update time for all fiat rates against the
// provided expiryTime. This only returns true if all rates are expired.
func (source *commonRateSource) isExpired(expiryTime time.Duration) bool {
	now := time.Now()

	source.mtx.RLock()
	defer source.mtx.RUnlock()
	if len(source.fiatRates) == 0 {
		return false
	}
	for _, rateInfo := range source.fiatRates {
		if now.Sub(rateInfo.lastUpdate) < expiryTime {
			return false // one not expired is enough
		}
	}
	return true
}

// assetRate returns the fiat rate information for the assetID specified. The
// fiatRateInfo returned should not be modified by the caller.
func (source *commonRateSource) assetRate(assetID uint32) *fiatRateInfo {
	source.mtx.RLock()
	defer source.mtx.RUnlock()
	return source.fiatRates[assetID]
}

// refreshRates updates the last update time and the rate information for assets.
func (source *commonRateSource) refreshRates(ctx context.Context, logger dex.Logger, assets map[uint32]*SupportedAsset) {
	fiatRates := source.fetchRates(ctx, logger, assets)
	now := time.Now()
	source.mtx.Lock()
	defer source.mtx.Unlock()
	for assetID, fiatRate := range fiatRates {
		if fiatRate <= 0 {
			continue
		}
		source.fiatRates[assetID] = &fiatRateInfo{
			rate:       fiatRate,
			lastUpdate: now,
		}
	}
}

// Used to initialize a fiat rate source.
func newCommonRateSource(fetcher rateFetcher) *commonRateSource {
	return &commonRateSource{
		fetchRates: fetcher,
		fiatRates:  make(map[uint32]*fiatRateInfo),
	}
}

// fetchCoinpaprikaRates retrieves and parses fiat rate data from the
// Coinpaprika API. See https://api.coinpaprika.com/#operation/getTickersById
// for sample request and response information.
func fetchCoinpaprikaRates(ctx context.Context, log dex.Logger, assets map[uint32]*SupportedAsset) map[uint32]float64 {
	fiatRates := make(map[uint32]float64)
	for assetID, sa := range assets {
		if sa.Wallet == nil {
			// we don't want to fetch rates for assets with no wallet.
			continue
		}

		res := new(struct {
			Quotes struct {
				Currency struct {
					Price float64 `json:"price"`
				} `json:"USD"`
			} `json:"quotes"`
		})

		symbol := sa.Symbol
		name := sa.Name
		// TODO: Store these within the *SupportedAsset.
		switch assetID {
		case 60001: // usdc
			symbol = "usdc"
			name = "usd-coin"
		}

		reqStr := fmt.Sprintf(coinpaprikaURL, coinpapSlug(symbol, name))

		if err := getRates(ctx, reqStr, res); err != nil {
			log.Error(err)
			continue
		}

		fiatRates[assetID] = res.Quotes.Currency.Price
	}
	return fiatRates
}

// fetchDcrdataRates retrieves and parses fiat rate data from dcrdata
// exchange rate API.
func fetchDcrdataRates(ctx context.Context, log dex.Logger, assets map[uint32]*SupportedAsset) map[uint32]float64 {
	assetBTC := assets[btcBipID]
	assetDCR := assets[dcrBipID]
	noBTCAsset := assetBTC == nil || assetBTC.Wallet == nil
	noDCRAsset := assetDCR == nil || assetDCR.Wallet == nil
	if noBTCAsset && noDCRAsset {
		return nil
	}

	fiatRates := make(map[uint32]float64)
	res := new(struct {
		DcrPrice float64 `json:"dcrPrice"`
		BtcPrice float64 `json:"btcPrice"`
	})

	if err := getRates(ctx, dcrDataURL, res); err != nil {
		log.Error(err)
		return nil
	}

	if !noBTCAsset {
		fiatRates[btcBipID] = res.BtcPrice
	}
	if !noDCRAsset {
		fiatRates[dcrBipID] = res.DcrPrice
	}

	return fiatRates
}

// fetchMessariRates retrieves and parses fiat rate data from the Messari API.
// See https://messari.io/api/docs#operation/Get%20Asset%20Market%20Data for
// sample request and response information.
func fetchMessariRates(ctx context.Context, log dex.Logger, assets map[uint32]*SupportedAsset) map[uint32]float64 {
	fiatRates := make(map[uint32]float64)
	for assetID, asset := range assets {
		if asset.Wallet == nil {
			// we don't want to fetch rate for assets with no wallet.
			continue
		}

		res := new(struct {
			Data struct {
				MarketData struct {
					Price float64 `json:"price_usd"`
				} `json:"market_data"`
			} `json:"data"`
		})

		slug := strings.ToLower(asset.Symbol)

		// TODO: Store these within the *SupportedAsset.
		switch assetID {
		case 60001: // usdc
			slug = "usdc"
		}

		reqStr := fmt.Sprintf(messariURL, slug)

		if err := getRates(ctx, reqStr, res); err != nil {
			log.Error(err)
			continue
		}

		fiatRates[assetID] = res.Data.MarketData.Price
	}
	return fiatRates
}

func coinpapSlug(symbol, name string) string {
	slug := fmt.Sprintf("%s-%s", symbol, name)
	// Special handling for asset names with multiple space, e.g Bitcoin Cash.
	return strings.ToLower(strings.ReplaceAll(slug, " ", "-"))
}

func getRates(ctx context.Context, url string, thing interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response, got status code %d", resp.StatusCode)
	}

	reader := io.LimitReader(resp.Body, 1<<22)
	return json.NewDecoder(reader).Decode(thing)
}
