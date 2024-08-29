// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/dexnet"
	"decred.org/dcrdex/dex/fiatrates"
)

const (
	oraclePriceExpiration = time.Minute * 10
	oracleRecheckInterval = time.Minute * 3

	// If the total USD volume of all oracles is less than
	// minimumUSDVolumeForOraclesAvg, the oracles will be ignored for
	// pricing averages.
	minimumUSDVolumeForOraclesAvg = 100_000
)

// MarketReport contains a market's rates on various exchanges and the fiat
// rates of the base/quote assets.
type MarketReport struct {
	Price         float64         `json:"price"`
	Oracles       []*OracleReport `json:"oracles"`
	BaseFiatRate  float64         `json:"baseFiatRate"`
	QuoteFiatRate float64         `json:"quoteFiatRate"`
	BaseFees      *LotFeeRange    `json:"baseFees"`
	QuoteFees     *LotFeeRange    `json:"quoteFees"`
}

// OracleReport is a summary of a market on an exchange.
type OracleReport struct {
	Host     string  `json:"host"`
	USDVol   float64 `json:"usdVol"`
	BestBuy  float64 `json:"bestBuy"`
	BestSell float64 `json:"bestSell"`
}

// stampedPrice is used for caching price data that can expire.
type cachedPrice struct {
	stamp   time.Time
	price   float64
	oracles []*OracleReport
}

type marketPair struct {
	baseID, quoteID uint32
}

func (m marketPair) String() string {
	return fmt.Sprintf("%s-%s", dex.BipIDSymbol(m.baseID), dex.BipIDSymbol(m.quoteID))
}

type syncedMarket struct {
	numSubscribers uint32
	stopSync       context.CancelFunc
}

type priceOracle struct {
	ctx context.Context
	log dex.Logger

	syncedMarketsMtx sync.RWMutex
	syncedMarkets    map[marketPair]*syncedMarket

	cachedPricesMtx sync.RWMutex
	cachedPrices    map[marketPair]*cachedPrice
}

func newPriceOracle(ctx context.Context, log dex.Logger) *priceOracle {
	oracle := &priceOracle{
		ctx:           ctx,
		cachedPrices:  make(map[marketPair]*cachedPrice),
		syncedMarkets: make(map[marketPair]*syncedMarket),
		log:           log,
	}

	go func() {
		<-ctx.Done()
		oracle.syncedMarketsMtx.Lock()
		defer oracle.syncedMarketsMtx.Unlock()
		for mkt, syncedMarket := range oracle.syncedMarkets {
			syncedMarket.stopSync()
			delete(oracle.syncedMarkets, mkt)
		}
	}()

	return oracle
}

type oracle interface {
	getMarketPrice(baseID, quoteID uint32) float64
}

var _ oracle = (*priceOracle)(nil)

// getMarketPrice returns the volume weighted market rate for the specified
// base/quote pair. This market rate is used as the "oracleRate" in the
// basic market making strategy.
func (o *priceOracle) getMarketPrice(baseID, quoteID uint32) float64 {
	price, _, err := o.getOracleInfo(baseID, quoteID)
	if err != nil {
		return 0
	}
	return price
}

func (o *priceOracle) getCachedPrice(baseID, quoteID uint32) *cachedPrice {
	o.cachedPricesMtx.RLock()
	defer o.cachedPricesMtx.RUnlock()
	return o.cachedPrices[marketPair{baseID, quoteID}]
}

// getOracleInfo returns the volume weighted market rate for a given base/quote pair
// and details about the market on each available exchange that was used to determine
// the market rate. This market rate is used as the "oracleRate" in the basic market
// making strategy.
func (o *priceOracle) getOracleInfo(baseID, quoteID uint32) (float64, []*OracleReport, error) {
	cachedPrice := o.getCachedPrice(baseID, quoteID)
	isAutoSyncing := o.marketIsAutoSyncing(baseID, quoteID)

	if isAutoSyncing {
		if cachedPrice == nil || time.Since(cachedPrice.stamp) > oraclePriceExpiration {
			return 0, nil, fmt.Errorf("auto-synced market has an expired price")
		}
		o.log.Tracef("Returning cached price of synced market %s", marketPair{baseID, quoteID})
		return cachedPrice.price, cachedPrice.oracles, nil
	}

	if cachedPrice != nil && time.Since(cachedPrice.stamp) < oracleRecheckInterval {
		o.log.Tracef("Returning cached price of non synced market %s", marketPair{baseID, quoteID})
		return cachedPrice.price, cachedPrice.oracles, nil
	}

	return o.syncMarket(baseID, quoteID)
}

func (o *priceOracle) marketIsAutoSyncing(baseID, quoteID uint32) bool {
	o.syncedMarketsMtx.RLock()
	defer o.syncedMarketsMtx.RUnlock()
	_, found := o.syncedMarkets[marketPair{baseID, quoteID}]
	return found
}

func (o *priceOracle) startAutoSyncingMarket(baseID, quoteID uint32) error {
	mkt := marketPair{baseID, quoteID}

	o.syncedMarketsMtx.Lock()
	defer o.syncedMarketsMtx.Unlock()

	if syncedMarket, found := o.syncedMarkets[mkt]; found {
		syncedMarket.numSubscribers++
		return nil
	}

	_, _, err := o.syncMarket(baseID, quoteID)
	if err != nil {
		return err
	}

	ctx, stopSync := context.WithCancel(o.ctx)
	go func() {
		timer := time.After(0)
		for {
			select {
			case <-timer:
				_, _, err := o.syncMarket(baseID, quoteID)
				if err != nil {
					o.log.Errorf("Error syncing market %s: %v", mkt, err)
					timer = time.After(30 * time.Second)
				} else {
					timer = time.After(oracleRecheckInterval)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	o.syncedMarkets[mkt] = &syncedMarket{
		numSubscribers: 1,
		stopSync:       stopSync,
	}

	return nil
}

func (o *priceOracle) stopAutoSyncingMarket(baseID, quoteID uint32) {
	mkt := marketPair{baseID, quoteID}

	o.syncedMarketsMtx.Lock()
	defer o.syncedMarketsMtx.Unlock()

	if syncedMarket, found := o.syncedMarkets[mkt]; found {
		syncedMarket.numSubscribers--
		if syncedMarket.numSubscribers == 0 {
			syncedMarket.stopSync()
			delete(o.syncedMarkets, mkt)
		}
	}
}

func (o *priceOracle) syncMarket(baseID, quoteID uint32) (float64, []*OracleReport, error) {
	mkt := marketPair{baseID, quoteID}
	price, oracles, err := fetchMarketPrice(o.ctx, baseID, quoteID, o.log)
	if err != nil {
		return 0, nil, fmt.Errorf("error fetching market price for %s: %v", mkt, err)
	}

	o.cachedPricesMtx.Lock()
	defer o.cachedPricesMtx.Unlock()

	o.cachedPrices[mkt] = &cachedPrice{
		stamp:   time.Now(),
		price:   price,   // Might be zero
		oracles: oracles, // might be empty
	}

	return price, oracles, nil
}

func coinpapAsset(assetID uint32) (*fiatrates.CoinpaprikaAsset, error) {
	if tkn := asset.TokenInfo(assetID); tkn != nil {
		symbol := dex.BipIDSymbol(assetID)
		symbol = strings.Split(symbol, ".")[0]
		return &fiatrates.CoinpaprikaAsset{
			AssetID: assetID,
			Name:    tkn.Name,
			Symbol:  symbol,
		}, nil
	}
	a := asset.Asset(assetID)
	if a == nil {
		return nil, fmt.Errorf("unknown asset ID %d", assetID)
	}
	return &fiatrates.CoinpaprikaAsset{
		AssetID: assetID,
		Name:    a.Info.Name,
		Symbol:  a.Symbol,
	}, nil
}

func fetchMarketPrice(ctx context.Context, baseID, quoteID uint32, log dex.Logger) (float64, []*OracleReport, error) {
	b, err := coinpapAsset(baseID)
	if err != nil {
		return 0, nil, err
	}

	q, err := coinpapAsset(quoteID)
	if err != nil {
		return 0, nil, err
	}

	oracles, err := oracleMarketReport(ctx, b, q, log)
	if err != nil {
		return 0, nil, err
	}

	price, usdVolume, err := oracleAverage(oracles, log)
	if err != nil {
		return 0, nil, err
	}
	if usdVolume < minimumUSDVolumeForOraclesAvg {
		log.Meter("oracle_low_volume_"+b.Symbol+"_"+q.Symbol, 12*time.Hour).Infof(
			"Rejecting oracle average price for %s. not enough volume (%.2f USD < %.2f)",
			b.Symbol+"_"+q.Symbol, usdVolume, float32(minimumUSDVolumeForOraclesAvg),
		)
		return 0, oracles, nil
	}
	return price, oracles, err
}

func oracleAverage(mkts []*OracleReport, log dex.Logger) (rate, usdVolume float64, _ error) {
	var weightedSum float64
	var n int
	for _, mkt := range mkts {
		n++
		weightedSum += mkt.USDVol * (mkt.BestBuy + mkt.BestSell) / 2
		usdVolume += mkt.USDVol
	}
	if usdVolume == 0 {
		return 0, 0, nil // No markets have data. OK.
	}

	rate = weightedSum / usdVolume
	// TODO: Require a minimum USD volume?
	log.Tracef("marketAveragedPrice: price calculated from %d markets: rate = %f, USD volume = %f", n, rate, usdVolume)
	return rate, usdVolume, nil
}

func getRates(ctx context.Context, url string, thing any) (err error) {
	return dexnet.Get(ctx, url, thing, dexnet.WithSizeLimit(1<<22))
}

func getHTTPWithCode(ctx context.Context, url string, thing any) (int, error) {
	var code int
	return code, dexnet.Get(ctx, url, thing, dexnet.WithSizeLimit(1<<22), dexnet.WithStatusFunc(func(c int) { code = c }))
}

// Truncates the URL to the domain name and TLD.
func shortHost(addr string) (string, error) {
	u, err := url.Parse(addr)
	if u == nil {
		return "", fmt.Errorf("error parsing URL %q: %v", addr, err)
	}
	// remove subdomains
	parts := strings.Split(u.Host, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("not enough URL parts: %q", u.Host)
	}
	return parts[len(parts)-2] + "." + parts[len(parts)-1], nil
}

// spread fetches market data and returns the best buy and sell prices.
// TODO: We may be able to do better. We could pull a small amount of market
// book data and do a VWAP-like integration of, say, 1 DEX lot's worth.
func spread(ctx context.Context, addr string, baseSymbol, quoteSymbol string, log dex.Logger) (sell, buy float64) {
	host, err := shortHost(addr)
	if err != nil {
		log.Error(err)
		return
	}
	s := spreaders[host]
	if s == nil {
		return 0, 0
	}
	sell, buy, err = s(ctx, baseSymbol, quoteSymbol, log)
	if err != nil {
		log.Meter("spread_"+addr, time.Hour*12).Errorf("Error getting spread from %q: %v", addr, err)
		return 0, 0
	}
	return sell, buy
}

// oracleMarketReport fetches oracle price, spread, and volume data for known
// exchanges for a market. This is done by fetching the market data from
// coinpaprika, looking for known exchanges in the results, then pulling the
// data directly from the exchange's public data API.
func oracleMarketReport(ctx context.Context, b, q *fiatrates.CoinpaprikaAsset, log dex.Logger) (oracles []*OracleReport, err error) {
	// They're going to return the quote prices in terms of USD, which is
	// sort of nonsense for a non-USD market like DCR-BTC.
	baseSlug := fiatrates.CoinpapSlug(b.Name, b.Symbol)
	quoteSlug := fiatrates.CoinpapSlug(q.Name, q.Symbol)

	type coinpapQuote struct {
		Price  float64 `json:"price"`
		Volume float64 `json:"volume_24h"`
	}

	type coinpapMarket struct {
		BaseCurrencyID  string                   `json:"base_currency_id"`
		QuoteCurrencyID string                   `json:"quote_currency_id"`
		MarketURL       string                   `json:"market_url"`
		LastUpdated     time.Time                `json:"last_updated"`
		TrustScore      string                   `json:"trust_score"` // TrustScore appears to be deprecated?
		Outlier         bool                     `json:"outlier"`
		Quotes          map[string]*coinpapQuote `json:"quotes"`
	}

	var rawMarkets []*coinpapMarket
	url := fmt.Sprintf("https://api.coinpaprika.com/v1/coins/%s/markets", baseSlug)
	if err := getRates(ctx, url, &rawMarkets); err != nil {
		return nil, err
	}

	convertIfNecessary := func(addr, slug string) string {
		s, _ := shortHost(addr)
		switch s {
		case "coinbase.com":
			switch slug {
			case "usd-us-dollars":
				return "usdc-usd-coin"
			}
		}
		return slug
	}

	// Create filter for desirable matches.
	marketMatches := func(mkt *coinpapMarket) bool {
		if mkt.TrustScore != "high" || mkt.Outlier {
			return false
		}

		if mkt.MarketURL == "" {
			return false
		}

		if time.Since(mkt.LastUpdated) > time.Minute*30 {
			return false
		}

		return (mkt.BaseCurrencyID == baseSlug && mkt.QuoteCurrencyID == quoteSlug) ||
			(mkt.BaseCurrencyID == quoteSlug && mkt.QuoteCurrencyID == baseSlug)
	}

	var filteredResults []*coinpapMarket
	for _, mkt := range rawMarkets {
		mkt.BaseCurrencyID = convertIfNecessary(mkt.MarketURL, mkt.BaseCurrencyID)
		mkt.QuoteCurrencyID = convertIfNecessary(mkt.MarketURL, mkt.QuoteCurrencyID)
		if marketMatches(mkt) {
			filteredResults = append(filteredResults, mkt)
		}
	}

	addMarket := func(mkt *coinpapMarket, buy, sell float64) {
		host, err := shortHost(mkt.MarketURL)
		if err != nil {
			log.Error(err)
			return
		}
		oracle := &OracleReport{
			Host:     host,
			BestBuy:  buy,
			BestSell: sell,
		}
		oracles = append(oracles, oracle)
		usdQuote, found := mkt.Quotes["USD"]
		if found {
			oracle.USDVol = usdQuote.Volume
		}
	}

	for _, mkt := range filteredResults {
		if mkt.BaseCurrencyID == baseSlug {
			buy, sell := spread(ctx, mkt.MarketURL, b.Symbol, q.Symbol, log)
			if buy > 0 && sell > 0 {
				// buy = 0, sell = 0 for any unknown markets
				addMarket(mkt, buy, sell)
			}
		} else {
			buy, sell := spread(ctx, mkt.MarketURL, q.Symbol, b.Symbol, log) // base and quote switched
			if buy > 0 && sell > 0 {
				addMarket(mkt, 1/sell, 1/buy) // inverted
			}
		}
	}

	return
}

// Spreader is a function that can generate market spread data for a known
// exchange.
type Spreader func(ctx context.Context, baseSymbol, quoteSymbol string, log dex.Logger) (sell, buy float64, err error)

var spreaders = map[string]Spreader{
	"binance.com":  fetchBinanceGlobalSpread,
	"binance.us":   fetchBinanceUSSpread,
	"coinbase.com": fetchCoinbaseSpread,
	"bittrex.com":  fetchBittrexSpread,
	"hitbtc.com":   fetchHitBTCSpread,
	"exmo.com":     fetchEXMOSpread,
}

var binanceGlobalIs451, binanceUSIs451 atomic.Bool

func fetchBinanceGlobalSpread(ctx context.Context, baseSymbol, quoteSymbol string, log dex.Logger) (sell, buy float64, err error) {
	if binanceGlobalIs451.Load() {
		return 0, 0, nil
	}
	return fetchBinanceSpread(ctx, baseSymbol, quoteSymbol, false, log)
}

func fetchBinanceUSSpread(ctx context.Context, baseSymbol, quoteSymbol string, log dex.Logger) (sell, buy float64, err error) {
	if binanceUSIs451.Load() {
		return 0, 0, nil
	}
	return fetchBinanceSpread(ctx, baseSymbol, quoteSymbol, true, log)
}

func fetchBinanceSpread(ctx context.Context, baseSymbol, quoteSymbol string, isUS bool, log dex.Logger) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	var url string
	if isUS {
		url = fmt.Sprintf("https://api.binance.us/api/v3/ticker/bookTicker?symbol=%s", slug)
	} else {
		url = fmt.Sprintf("https://api.binance.com/api/v3/ticker/bookTicker?symbol=%s", slug)
	}

	var resp struct {
		BidPrice float64 `json:"bidPrice,string"`
		AskPrice float64 `json:"askPrice,string"`
	}

	code, err := getHTTPWithCode(ctx, url, &resp)
	if err != nil {
		if code == http.StatusUnavailableForLegalReasons {
			if isUS && binanceUSIs451.CompareAndSwap(false, true) {
				log.Debugf("Binance U.S. responded with a 451. Disabling")
			} else if !isUS && binanceGlobalIs451.CompareAndSwap(false, true) {
				log.Debugf("Binance Global responded with a 451. Disabling")
			}
			return 0, 0, nil
		}
		return 0, 0, err
	}

	return resp.AskPrice, resp.BidPrice, nil
}

func fetchCoinbaseSpread(ctx context.Context, baseSymbol, quoteSymbol string, _ dex.Logger) (sell, buy float64, err error) {
	slugSymbol := func(symbol string) string {
		switch symbol {
		case "usdc":
			return "USD"
		}
		return strings.ToUpper(symbol)
	}
	slug := fmt.Sprintf("%s-%s", slugSymbol(baseSymbol), slugSymbol(quoteSymbol))
	url := fmt.Sprintf("https://api.exchange.coinbase.com/products/%s/ticker", slug)

	var resp struct {
		Ask float64 `json:"ask,string"`
		Bid float64 `json:"bid,string"`
	}

	return resp.Ask, resp.Bid, getRates(ctx, url, &resp)
}

func fetchBittrexSpread(ctx context.Context, baseSymbol, quoteSymbol string, _ dex.Logger) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s-%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.bittrex.com/v3/markets/%s/ticker", slug)
	var resp struct {
		AskRate float64 `json:"askRate,string"`
		BidRate float64 `json:"bidRate,string"`
	}
	return resp.AskRate, resp.BidRate, getRates(ctx, url, &resp)
}

func fetchHitBTCSpread(ctx context.Context, baseSymbol, quoteSymbol string, _ dex.Logger) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.hitbtc.com/api/3/public/orderbook/%s?depth=1", slug)

	var resp struct {
		Ask [][2]json.Number `json:"ask"`
		Bid [][2]json.Number `json:"bid"`
	}
	if err := getRates(ctx, url, &resp); err != nil {
		return 0, 0, err
	}
	if len(resp.Ask) < 1 || len(resp.Bid) < 1 {
		return 0, 0, fmt.Errorf("not enough orders")
	}

	ask, err := resp.Ask[0][0].Float64()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to decode ask price %q", resp.Ask[0][0])
	}

	bid, err := resp.Bid[0][0].Float64()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to decode bid price %q", resp.Bid[0][0])
	}

	return ask, bid, nil
}

func fetchEXMOSpread(ctx context.Context, baseSymbol, quoteSymbol string, _ dex.Logger) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s_%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.exmo.com/v1.1/order_book?pair=%s&limit=1", slug)

	var resp map[string]*struct {
		AskTop float64 `json:"ask_top,string"`
		BidTop float64 `json:"bid_top,string"`
	}

	if err := getRates(ctx, url, &resp); err != nil {
		return 0, 0, err
	}

	mkt := resp[slug]
	if mkt == nil {
		return 0, 0, errors.New("slug not in response")
	}

	return mkt.AskTop, mkt.BidTop, nil
}
