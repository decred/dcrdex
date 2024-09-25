package fiatrates

import (
	"context"
	"fmt"
	"strings"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/dexnet"
)

const (
	coinpaprikaURL     = "https://api.coinpaprika.com/v1/tickers"
	fiatRequestTimeout = time.Second * 5
)

func CoinpapSlug(name, symbol string) string {
	name, symbol = parseCoinpapNameSymbol(name, symbol)
	slug := fmt.Sprintf("%s-%s", symbol, name)
	// Special handling for asset names with multiple space, e.g Bitcoin Cash.
	return strings.ToLower(strings.ReplaceAll(slug, " ", "-"))
}

type CoinpaprikaAsset struct {
	AssetID uint32
	Name    string
	Symbol  string
}

func parseCoinpapNameSymbol(name, symbol string) (string, string) {
	parts := strings.Split(symbol, ".")
	network := symbol
	if len(parts) == 2 {
		symbol, network = parts[0], parts[1]
	}
	switch symbol {
	case "usdc":
		name = "usd-coin"
	case "polygon":
		symbol = "matic"
		name = "polygon"
	case "weth":
		name = "weth"
	case "matic":
		switch network {
		case "eth":
			symbol, name = "matic", "polygon"
		}
	}
	return name, symbol
}

// FetchCoinpaprikaRates retrieves and parses fiat rate data from the
// Coinpaprika API. See https://api.coinpaprika.com/#operation/getTickersById
// for sample request and response information.
func FetchCoinpaprikaRates(ctx context.Context, assets []*CoinpaprikaAsset, log dex.Logger) map[uint32]float64 {
	fiatRates := make(map[uint32]float64)
	slugAssets := make(map[string][]uint32)
	for _, a := range assets {
		slug := CoinpapSlug(a.Name, a.Symbol)
		slugAssets[slug] = append(slugAssets[slug], a.AssetID)
	}

	ctx, cancel := context.WithTimeout(ctx, fiatRequestTimeout)
	defer cancel()

	var res []*struct {
		ID     string `json:"id"`
		Quotes struct {
			USD struct {
				Price float64 `json:"price"`
			} `json:"USD"`
		} `json:"quotes"`
	}

	if err := getRates(ctx, coinpaprikaURL, &res); err != nil {
		log.Errorf("Error getting fiat exchange rates from coinpaprika: %v", err)
		return fiatRates
	}
	for _, coinInfo := range res {
		assetIDs, found := slugAssets[coinInfo.ID]
		if !found {
			continue
		}

		price := coinInfo.Quotes.USD.Price
		if price == 0 {
			log.Errorf("zero-price returned from coinpaprika for slug %s", coinInfo.ID)
			continue
		}
		for _, assetID := range assetIDs {
			fiatRates[assetID] = price
		}
	}
	return fiatRates
}

func getRates(ctx context.Context, uri string, thing any) error {
	return dexnet.Get(ctx, uri, thing, dexnet.WithSizeLimit(1<<22))
}
