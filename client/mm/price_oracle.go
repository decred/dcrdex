// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/fiatrates"
)

const (
	oraclePriceExpiration = time.Minute * 10
	oracleRecheckInterval = time.Minute * 3

	errNoMarkets = dex.ErrorKind("no markets")
	// an autosync oracle will return errUnsyncedMarket if the price oracle
	// was not initialized with the market that it was queried for.
	errUnsyncedMarket = dex.ErrorKind("market not synced")
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
	mtx     sync.RWMutex
	stamp   time.Time
	price   float64
	oracles []*OracleReport

	base  *fiatrates.CoinpaprikaAsset
	quote *fiatrates.CoinpaprikaAsset
}

// priceOracle periodically fetches market prices from a set of oracles.
// The priceOracle may be synced or unsynced. A synced priceOracle will
// periodically fetch market prices for all markets that it was initialized
// with. An unsynced priceOracle will only fetch market prices when they are
// requested.
type priceOracle struct {
	ctx      context.Context
	log      dex.Logger
	autoSync bool

	cachedPricesMtx sync.RWMutex
	cachedPrices    map[string]*cachedPrice
}

type oracle interface {
	getMarketPrice(base, quote uint32) float64
}

var _ oracle = (*priceOracle)(nil)

func (o *priceOracle) getOracleDataAutoSync(baseID, quoteID uint32) (float64, []*OracleReport, error) {
	mktStr := (&mkt{baseID, quoteID}).String()

	o.cachedPricesMtx.RLock()
	price, ok := o.cachedPrices[mktStr]
	o.cachedPricesMtx.RUnlock()

	if !ok {
		return 0, nil, errUnsyncedMarket
	}

	price.mtx.RLock()
	defer price.mtx.RUnlock()

	if time.Since(price.stamp) < oraclePriceExpiration {
		return price.price, price.oracles, nil
	}

	return 0, nil, fmt.Errorf("expired price data for %s", mktStr)
}

func (o *priceOracle) getOracleDataNoAutoSync(baseID, quoteID uint32) (float64, []*OracleReport, error) {
	mktStr := (&mkt{baseID, quoteID}).String()

	o.cachedPricesMtx.Lock()
	price, ok := o.cachedPrices[mktStr]
	if !ok {
		cp, err := newCachedPrice(baseID, quoteID)
		if err != nil {
			o.cachedPricesMtx.Unlock()
			return 0, nil, err
		}
		o.cachedPrices[mktStr] = cp
		price = cp
	}
	o.cachedPricesMtx.Unlock()

	price.mtx.RLock()
	if time.Since(price.stamp) < oracleRecheckInterval {
		defer price.mtx.RUnlock()
		return price.price, price.oracles, nil
	}
	price.mtx.RUnlock()

	err := o.syncMarket(mktStr)
	if err != nil {
		return 0, nil, err
	}

	price.mtx.RLock()
	defer price.mtx.RUnlock()
	return price.price, price.oracles, nil
}

// getMarketPrice returns the volume weighted market rate for the specified
// base/quote pair. This market rate is used as the "oracleRate" in the
// basic market making strategy.
func (o *priceOracle) getMarketPrice(baseID, quoteID uint32) float64 {
	var price float64
	var err error
	if o.autoSync {
		price, _, err = o.getOracleDataAutoSync(baseID, quoteID)
	} else {
		price, _, err = o.getOracleDataNoAutoSync(baseID, quoteID)
	}
	if err != nil {
		o.log.Errorf("error fetching market price for %d-%d: %v", baseID, quoteID, err)
		return 0
	}
	return price
}

// getOracleInfo returns the volume weighted market rate for a given base/quote pair
// and details about the market on each available exchange that was used to determine
// the market rate. This market rate is used as the "oracleRate" in the basic market
// making strategy.
func (o *priceOracle) getOracleInfo(baseID, quoteID uint32) (float64, []*OracleReport, error) {
	var price float64
	var oracles []*OracleReport
	var err error
	if o.autoSync {
		price, oracles, err = o.getOracleDataAutoSync(baseID, quoteID)
	} else {
		price, oracles, err = o.getOracleDataNoAutoSync(baseID, quoteID)
	}
	if err != nil {
		o.log.Errorf("error fetching market report for %d-%d: %v", baseID, quoteID, err)
		return 0, nil, err
	}
	return price, oracles, nil
}

type mkt struct {
	baseID, quoteID uint32
}

func (mkt *mkt) String() string {
	return fmt.Sprintf("%d-%d", mkt.baseID, mkt.quoteID)
}

func coinpapAsset(assetID uint32) (*fiatrates.CoinpaprikaAsset, error) {
	if tkn := asset.TokenInfo(assetID); tkn != nil {
		return &fiatrates.CoinpaprikaAsset{
			AssetID: assetID,
			Name:    tkn.Name,
			Symbol:  dex.BipIDSymbol(assetID),
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

func newCachedPrice(baseID, quoteID uint32) (*cachedPrice, error) {
	b, err := coinpapAsset(baseID)
	if err != nil {
		return nil, err
	}

	q, err := coinpapAsset(quoteID)
	if err != nil {
		return nil, err
	}

	return &cachedPrice{
		base:  b,
		quote: q,
	}, nil
}

// newAutoSyncPriceOracle creates a new priceOracle that periodically fetches
// market prices from a set of oracles. Only the provided markets will be synced
// and only those markets will be available for querying.
func newAutoSyncPriceOracle(ctx context.Context, markets []*mkt, log dex.Logger) (*priceOracle, error) {
	cachedPrices := make(map[string]*cachedPrice)

	for _, mkt := range markets {
		if _, ok := cachedPrices[mkt.String()]; ok {
			log.Warnf("duplicate market: %s", mkt.String())
			continue
		}

		cachedPrice, err := newCachedPrice(mkt.baseID, mkt.quoteID)
		if err != nil {
			return nil, err
		}
		cachedPrices[mkt.String()] = cachedPrice
	}

	oracle := &priceOracle{
		ctx:          ctx,
		cachedPrices: cachedPrices,
		log:          log,
		autoSync:     true,
	}

	// Sync all markets on startup
	oracle.syncAllMarkets()

	go func() {
		ticker := time.NewTicker(oracleRecheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				oracle.syncAllMarkets()
			case <-ctx.Done():
				return
			}
		}
	}()

	return oracle, nil
}

// newUnsyncedPriceOracle creates a new priceOracle that only fetches market prices
// when they are requested. Prices will be cached for a short time.
func newUnsyncedPriceOracle(log dex.Logger) *priceOracle {
	return &priceOracle{
		ctx:          context.Background(),
		cachedPrices: make(map[string]*cachedPrice),
		log:          log,
	}
}

// syncMarket fetches the latest price for a market. There must
// be an entry for the market cachedPrices before this function
// is called.
func (o *priceOracle) syncMarket(mkt string) error {
	o.cachedPricesMtx.RLock()
	cachedPrice := o.cachedPrices[mkt]
	o.cachedPricesMtx.RUnlock()
	if cachedPrice == nil {
		return fmt.Errorf("market %s not found", mkt)
	}

	cachedPrice.mtx.RLock()
	baseAsset := cachedPrice.base
	quoteAsset := cachedPrice.quote
	cachedPrice.mtx.RUnlock()

	price, oracles, err := fetchMarketPrice(o.ctx, baseAsset, quoteAsset, o.log)
	if err != nil {
		return fmt.Errorf("error fetching market price for %s: %v", mkt, err)
	}

	o.log.Debugf("fetched market price for %s: %f (%d oracles)", mkt, price, len(oracles))

	cachedPrice.mtx.Lock()
	defer cachedPrice.mtx.Unlock()
	cachedPrice.price = price
	cachedPrice.oracles = oracles
	cachedPrice.stamp = time.Now()

	return nil
}

// syncAllMarkets fetches the latest prices for all markets.
func (o *priceOracle) syncAllMarkets() {
	wg := new(sync.WaitGroup)

	for mktName := range o.cachedPrices {
		wg.Add(1)

		go func(mkt string) {
			defer wg.Done()
			err := o.syncMarket(mkt)
			if err != nil {
				o.log.Error(err)
			}
		}(mktName)
	}

	wg.Wait()
}

func fetchMarketPrice(ctx context.Context, b, q *fiatrates.CoinpaprikaAsset, log dex.Logger) (float64, []*OracleReport, error) {
	oracles, err := oracleMarketReport(ctx, b, q, log)
	if err != nil {
		return 0, nil, err
	}
	price, err := oracleAverage(oracles, log)
	if err != nil && !errors.Is(err, errNoMarkets) {
		return 0, nil, err
	}

	return price, oracles, nil
}

func oracleAverage(mkts []*OracleReport, log dex.Logger) (float64, error) {
	var weightedSum, usdVolume float64
	var n int
	for _, mkt := range mkts {
		n++
		weightedSum += mkt.USDVol * (mkt.BestBuy + mkt.BestSell) / 2
		usdVolume += mkt.USDVol
	}
	if usdVolume == 0 {
		log.Tracef("marketAveragedPrice: no markets")
		return 0, errNoMarkets
	}

	rate := weightedSum / usdVolume
	// TODO: Require a minimum USD volume?
	log.Tracef("marketAveragedPrice: price calculated from %d markets: rate = %f, USD volume = %f", n, rate, usdVolume)
	return rate, nil
}

func coinpapSlug(symbol, name string) string {
	name, symbol = fiatrates.ParseCoinpapNameSymbol(name, symbol)
	slug := fmt.Sprintf("%s-%s", symbol, name)
	// Special handling for asset names with multiple space, e.g Bitcoin Cash.
	return strings.ToLower(strings.ReplaceAll(slug, " ", "-"))
}

func getRates(ctx context.Context, url string, thing any) error {
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
	sell, buy, err = s(ctx, baseSymbol, quoteSymbol)
	if err != nil {
		log.Errorf("Error getting spread from %q: %v", addr, err)
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
	baseSlug := coinpapSlug(b.Symbol, b.Name)
	quoteSlug := coinpapSlug(q.Symbol, q.Name)

	type coinpapQuote struct {
		Price  float64 `json:"price"`
		Volume float64 `json:"volume_24h"`
	}

	type coinpapMarket struct {
		BaseCurrencyID  string                   `json:"base_currency_id"`
		QuoteCurrencyID string                   `json:"quote_currency_id"`
		MarketURL       string                   `json:"market_url"`
		LastUpdated     time.Time                `json:"last_updated"`
		TrustScore      string                   `json:"trust_score"`
		Quotes          map[string]*coinpapQuote `json:"quotes"`
	}

	var rawMarkets []*coinpapMarket
	url := fmt.Sprintf("https://api.coinpaprika.com/v1/coins/%s/markets", baseSlug)
	if err := getRates(ctx, url, &rawMarkets); err != nil {
		return nil, err
	}

	// Create filter for desirable matches.
	marketMatches := func(mkt *coinpapMarket) bool {
		if mkt.TrustScore != "high" {
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
type Spreader func(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error)

var spreaders = map[string]Spreader{
	"binance.com":  fetchBinanceSpread,
	"coinbase.com": fetchCoinbaseSpread,
	"bittrex.com":  fetchBittrexSpread,
	"hitbtc.com":   fetchHitBTCSpread,
	"exmo.com":     fetchEXMOSpread,
}

func fetchBinanceSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/bookTicker?symbol=%s", slug)

	var resp struct {
		BidPrice float64 `json:"bidPrice,string"`
		AskPrice float64 `json:"askPrice,string"`
	}
	return resp.AskPrice, resp.BidPrice, getRates(ctx, url, &resp)
}

func fetchCoinbaseSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s-%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.exchange.coinbase.com/products/%s/ticker", slug)

	var resp struct {
		Ask float64 `json:"ask,string"`
		Bid float64 `json:"bid,string"`
	}

	return resp.Ask, resp.Bid, getRates(ctx, url, &resp)
}

func fetchBittrexSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
	slug := fmt.Sprintf("%s-%s", strings.ToUpper(baseSymbol), strings.ToUpper(quoteSymbol))
	url := fmt.Sprintf("https://api.bittrex.com/v3/markets/%s/ticker", slug)
	var resp struct {
		AskRate float64 `json:"askRate,string"`
		BidRate float64 `json:"bidRate,string"`
	}
	return resp.AskRate, resp.BidRate, getRates(ctx, url, &resp)
}

func fetchHitBTCSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
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

func fetchEXMOSpread(ctx context.Context, baseSymbol, quoteSymbol string) (sell, buy float64, err error) {
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
