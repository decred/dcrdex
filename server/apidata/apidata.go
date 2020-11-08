// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package apidata

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/matcher"
)

const (
	// DefaultCandleRequest is the number of candles to return if the request
	// does not specify otherwise.
	DefaultCandleRequest = 50
	// CacheSize is the default cache size. Also represents the maximum number
	// of candles that can be requested at once.
	CacheSize = 1000
)

var (
	// BinSizes is the default bin sizes for candlestick data sets. Exported for
	// use in the 'config' response. Internally, we will parse these to uint64
	// milliseconds.
	BinSizes = []string{"24h", "1h", "15m"}
	// Our internal millisecond representation of the bin sizes.
	binSizes []uint64
	started  uint32
)

// DBSource is a source of persistent data. DBSource is used to prime the
// caches at startup.
type DBSource interface {
	LoadEpochStats(base, quote uint32, caches []*db.CandleCache) error
}

// MarketSource is a source of market information. Markets are added after
// construction but before use using the AddMarketSource method.
type MarketSource interface {
	EpochDuration() uint64
	Base() uint32
	Quote() uint32
}

// BookSource is a source of order book information. The BookSource is added
// after construction but before use.
type BookSource interface {
	Book(mktName string) (*msgjson.OrderBook, error)
}

// DataAPI implement a data API backend. API data is
type DataAPI struct {
	db             DBSource
	epochDurations map[string]uint64
	bookSource     BookSource

	spotsMtx sync.RWMutex
	spots    map[string]json.RawMessage

	cacheMtx     sync.RWMutex
	marketCaches map[string]map[uint64]*db.CandleCache
}

// NewDataAPI is the constructor for a new DataAPI.
func NewDataAPI(ctx context.Context, dbSrc DBSource) *DataAPI {
	s := &DataAPI{
		db:             dbSrc,
		epochDurations: make(map[string]uint64),
		spots:          make(map[string]json.RawMessage),
		marketCaches:   make(map[string]map[uint64]*db.CandleCache),
	}

	if atomic.CompareAndSwapUint32(&started, 0, 1) {
		comms.RegisterHTTP(msgjson.SpotsRoute, s.handleSpots)
		comms.RegisterHTTP(msgjson.CandlesRoute, s.handleCandles)
		comms.RegisterHTTP(msgjson.OrderBookRoute, s.handleOrderBook)
	}
	return s
}

// AddMarketSource should be called before any markets are running.
func (s *DataAPI) AddMarketSource(mkt MarketSource) error {
	mktName, err := dex.MarketName(mkt.Base(), mkt.Quote())
	if err != nil {
		return err
	}
	epochDur := mkt.EpochDuration()
	s.epochDurations[mktName] = epochDur
	binCaches := make(map[uint64]*db.CandleCache, len(binSizes)+1)
	s.marketCaches[mktName] = binCaches
	cacheList := make([]*db.CandleCache, 0, len(binSizes)+1)
	for _, binSize := range append([]uint64{epochDur}, binSizes...) {
		cache := db.NewCandleCache(CacheSize, binSize)
		cacheList = append(cacheList, cache)
		binCaches[binSize] = cache
	}
	err = s.db.LoadEpochStats(mkt.Base(), mkt.Quote(), cacheList)
	if err != nil {
		return err
	}
	return nil
}

// SetBookSource should be called before the first call to handleBook.
func (s *DataAPI) SetBookSource(bs BookSource) {
	s.bookSource = bs
}

// ReportEpoch should be called by every Market after every match cycle to
// report their epoch stats.
func (s *DataAPI) ReportEpoch(base, quote uint32, epochIdx uint64, stats *matcher.MatchCycleStats) error {
	mktName, err := dex.MarketName(base, quote)
	if err != nil {
		return err
	}

	// Add the candlestick.
	s.cacheMtx.Lock()
	mktCaches := s.marketCaches[mktName]
	if mktCaches == nil {
		s.cacheMtx.Unlock()
		return fmt.Errorf("unkown market %q", mktName)
	}
	epochDur := s.epochDurations[mktName]
	startStamp := epochIdx * epochDur
	endStamp := startStamp + epochDur
	for _, cache := range mktCaches {
		cache.Add(&db.Candle{
			StartStamp:  startStamp,
			EndStamp:    endStamp,
			MatchVolume: stats.MatchVolume,
			QuoteVolume: stats.QuoteVolume,
			BookVolume:  stats.BookVolume,
			OrderVolume: stats.OrderVolume,
			HighRate:    stats.HighRate,
			LowRate:     stats.LowRate,
			StartRate:   stats.StartRate,
			EndRate:     stats.EndRate,
		})
	}
	s.cacheMtx.Unlock()

	// Encode the spot price.
	s.spotsMtx.Lock()
	s.spots[mktName], err = json.Marshal(msgjson.Spot{
		Stamp:      encode.UnixMilliU(time.Now()),
		BaseID:     base,
		QuoteID:    quote,
		Rate:       stats.EndRate,
		BookVolume: stats.BookVolume,
	})
	s.spotsMtx.Unlock()
	return err
}

// handleSpots implements comms.HTTPHandler for the /spots endpoint.
func (s *DataAPI) handleSpots(interface{}) (interface{}, error) {
	s.spotsMtx.RLock()
	defer s.spotsMtx.RUnlock()
	spots := make([]json.RawMessage, 0, len(s.spots))
	for _, spot := range s.spots {
		spots = append(spots, spot)
	}
	return spots, nil
}

// handleCandles implements comms.HTTPHandler for the /candles endpoints.
func (s *DataAPI) handleCandles(thing interface{}) (interface{}, error) {
	req, ok := thing.(*msgjson.CandlesRequest)
	if !ok {
		return nil, fmt.Errorf("candles request unparseable")
	}

	if req.NumCandles == 0 {
		req.NumCandles = DefaultCandleRequest
	} else if req.NumCandles > CacheSize {
		return nil, fmt.Errorf("requested numCandles %d exceeds maximum request size %d", req.NumCandles, CacheSize)
	}

	mkt, err := dex.MarketName(req.BaseID, req.QuoteID)
	if err != nil {
		return nil, fmt.Errorf("error parsing market for %d - %d", req.BaseID, req.QuoteID)
	}

	binSizeDuration, err := time.ParseDuration(req.BinSize)
	if err != nil {
		return nil, fmt.Errorf("error parsing binSize")
	}
	binSize := uint64(binSizeDuration / time.Millisecond)

	s.cacheMtx.RLock()
	defer s.cacheMtx.RUnlock()
	marketCaches := s.marketCaches[mkt]
	if marketCaches == nil {
		return nil, fmt.Errorf("market %s not known", mkt)
	}

	cache := marketCaches[binSize]
	if cache == nil {
		return nil, fmt.Errorf("no data available for binSize %s", req.BinSize)
	}

	return cache.WireCandles(req.NumCandles), nil
}

// handleOrderBook implements comms.HTTPHandler for the /orderbook endpoints.
func (s *DataAPI) handleOrderBook(thing interface{}) (interface{}, error) {
	req, ok := thing.(*msgjson.OrderBookSubscription)
	if !ok {
		return nil, fmt.Errorf("orderbook request unparseable")
	}

	mkt, err := dex.MarketName(req.Base, req.Quote)
	if err != nil {
		return nil, fmt.Errorf("can't parse requested market")
	}
	return s.bookSource.Book(mkt)
}

func init() {
	for _, s := range BinSizes {
		dur, err := time.ParseDuration(s)
		if err != nil {
			panic("error parsing bin size '" + s + "': " + err.Error())
		}
		binSizes = append(binSizes, uint64(dur/time.Millisecond))
	}
}
