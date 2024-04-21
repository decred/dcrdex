package fiatrates

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/ethereum/go-ethereum/log"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	defaultRefreshInterval = 5 * time.Minute
	messariRefreshInterval = 10 * time.Minute

	cryptoCompare              = "CryptoCompare"
	cryptoComparePriceEndpoint = "https://min-api.cryptocompare.com/data/pricemulti?fsyms=%s&tsyms=USD"

	binance                = "Binance"
	binancePriceEndpoint   = "https://api3.binance.com/api/v3/ticker/price?symbols=[%s]"
	binanceUSPriceEndpoint = "https://api.binance.us/api/v3/ticker/price?symbols=[%s]"

	coinpaprika              = "Coinparika"
	coinpaprikaPriceEndpoint = "https://api.coinpaprika.com/v1/tickers"

	messari              = "Messari"
	messariPriceEndpoint = "https://data.messari.io/api/v1/assets/%s/metrics/market-data"

	kuCoin              = "KuCoin"
	kuCoinPriceEndpoint = "https://api.kucoin.com/api/v1/prices?currencies=%s"
)

var (
	upperCaser = cases.Upper(language.AmericanEnglish)
)

func fiatSources(cfg Config) []*source {
	disabledSources := strings.ToLower(cfg.DisabledFiatSources)
	sources := []*source{
		{
			name:            cryptoCompare,
			requestInterval: defaultRefreshInterval,
			disabled:        cfg.CryptoCompareAPIKey == "" || strings.Contains(disabledSources, strings.ToLower(cryptoCompare)),
			getRates: func(ctx context.Context, assets []string, _ dex.Logger) (map[string]float64, error) {
				if cfg.CryptoCompareAPIKey == "" {
					return nil, nil // nothing to do
				}

				reqURL := fmt.Sprintf(cryptoComparePriceEndpoint, parseSymbols(assets...))
				response := make(map[string]map[string]float64)
				err := getRatesWithHeader(ctx, reqURL, &response, map[string]string{})
				if err != nil {
					return nil, fmt.Errorf("unable to fetch fiat rates: %w", err)
				}

				fiatRates := make(map[string]float64)
				for sym, rates := range response {
					rate, ok := rates["USD"]
					if ok {
						fiatRates[parseSymbol(sym)] = rate
					}
				}

				return fiatRates, nil
			},
		},
		{
			name:            kuCoin,
			requestInterval: defaultRefreshInterval,
			disabled:        strings.Contains(disabledSources, strings.ToLower(kuCoin)),
			getRates: func(ctx context.Context, assets []string, _ dex.Logger) (map[string]float64, error) {
				response := struct {
					Data map[string]string `json:"data"`
				}{}

				reqURL := fmt.Sprintf(kuCoinPriceEndpoint, parseSymbols(assets...))
				err := getRates(ctx, reqURL, &response)
				if err != nil {
					return nil, fmt.Errorf("unable to fetch fiat rates: %w", err)
				}

				fiatRates := make(map[string]float64)
				for sym, rateStr := range response.Data {
					rate, err := strconv.ParseFloat(rateStr, 64)
					if err != nil {
						log.Error("%s: failed to convert fiat rate for %s to float64: %v", kuCoin, sym, err)
						continue
					}
					fiatRates[parseSymbol(sym)] = rate
				}

				return fiatRates, nil
			},
		},
		{
			name:            binance,
			requestInterval: defaultRefreshInterval,
			disabled:        strings.Contains(disabledSources, strings.ToLower(binance)),
			getRates: func(ctx context.Context, assets []string, _ dex.Logger) (map[string]float64, error) {
				priceEndpoint := binancePriceEndpoint
				if cfg.EnableBinanceUS {
					priceEndpoint = binanceUSPriceEndpoint
				}

				symbols := parseBinanceSymbols(assets)
				if symbols == "" {
					return nil, nil // nothing to fetch
				}

				var response []*struct {
					Symbol string `json:"symbol"`
					Price  string `json:"price"`
				}

				reqURL := fmt.Sprintf(priceEndpoint, url.PathEscape(symbols))
				err := getRates(ctx, reqURL, &response)
				if err != nil {
					return nil, fmt.Errorf("unable to fetch fiat rates: %w", err)
				}

				fiatRates := make(map[string]float64)
				for _, asset := range response {
					symbol := parseSymbol(strings.TrimSuffix(asset.Symbol, "USDT"))
					rate, err := strconv.ParseFloat(asset.Price, 64)
					if err != nil {
						log.Error("%s: failed to convert fiat rate for %s to float64: %v", binance, symbol, err)
						continue
					}
					fiatRates[symbol] = rate
				}

				return fiatRates, nil
			},
		},
		{
			name:            coinpaprika,
			requestInterval: defaultRefreshInterval,
			disabled:        strings.Contains(disabledSources, strings.ToLower(coinpaprika)),
			getRates: func(ctx context.Context, assets []string, log dex.Logger) (map[string]float64, error) {
				fiatRates := make(map[string]float64, len(assets))
				for _, a := range assets {
					fiatRates[parseSymbol(a)] = 0
				}

				var res []*struct {
					Symbol string `json:"symbol"`
					Quotes struct {
						USD struct {
							Price float64 `json:"price"`
						} `json:"USD"`
					} `json:"quotes"`
				}

				if err := getRates(ctx, coinpaprikaPriceEndpoint, &res); err != nil {
					return nil, err
				}

				for _, coinInfo := range res {
					symbol := parseSymbol(coinInfo.Symbol)
					_, found := fiatRates[symbol]
					if !found {
						continue
					}

					price := coinInfo.Quotes.USD.Price
					if price == 0 {
						log.Errorf("zero-price returned from coinpaprika for asset with symbol %s", symbol)
						continue
					}

					fiatRates[symbol] = price
				}

				return fiatRates, nil
			},
		},
		{
			name:            messari,
			requestInterval: messariRefreshInterval,
			disabled:        strings.Contains(disabledSources, strings.ToLower(messari)),
			getRates: func(ctx context.Context, assets []string, log dex.Logger) (map[string]float64, error) {
				fiatRates := make(map[string]float64)
				for _, sym := range assets {
					res := new(struct {
						Data struct {
							MarketData struct {
								Price float64 `json:"price_usd"`
							} `json:"market_data"`
						} `json:"data"`
					})

					reqURL := fmt.Sprintf(messariPriceEndpoint, parseSymbols(sym))
					if err := getRates(ctx, reqURL, res); err != nil {
						log.Errorf("Error getting fiat exchange rates from messari: %v", err)
						continue // fetch other assets
					}

					fiatRates[parseSymbol(sym)] = res.Data.MarketData.Price
				}

				return fiatRates, nil
			},
		},
	}

	for i := range sources {
		sources[i].canReactivate = !sources[i].disabled
	}

	return sources
}

func parseSymbols(assets ...string) string {
	var symbols string
	for _, sym := range assets {
		symbols += parseSymbol(sym) + ","
	}
	return strings.Trim(symbols, ",")
}

func parseSymbol(sym string) string {
	if strings.EqualFold(sym, "polygon") {
		sym = "matic"
	} else if strings.EqualFold(sym, "usdc.eth") || strings.EqualFold(sym, "usdc.polygon") {
		sym = "usdc"
	}
	return upperCaser.String(sym)
}

func parseBinanceSymbols(assets []string) string {
	var symbols string
	for _, sym := range assets {
		sym = parseSymbol(sym)
		if strings.EqualFold(sym, "zcl") { // not supported on binance as of writing
			continue
		}
		symbols += fmt.Sprintf("%q,", sym+"USDT")
	}

	return strings.Trim(symbols, ",")
}
