// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package apidata

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/matcher"
)

var (
	// Our internal millisecond representation of the bin sizes.
	binSizes []uint64
	bin5min  uint64 = 60 * 5 * 1000
	started  uint32
)

// DBSource is a source of persistent data. DBSource is used to prime the
// caches at startup.
type DBSource interface {
	LoadEpochStats(base, quote uint32, caches []*candles.Cache) error
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

// DataAPI is a data API backend.
type DataAPI struct {
	db             DBSource
	epochDurations map[string]uint64
	bookSource     BookSource

	spotsMtx sync.RWMutex
	spots    map[string]json.RawMessage

	cacheMtx     sync.RWMutex
	marketCaches map[string]map[uint64]*candles.Cache
	cache5min    *candles.Cache
}

// NewDataAPI is the constructor for a new DataAPI.
func NewDataAPI(dbSrc DBSource) *DataAPI {
	s := &DataAPI{
		db:             dbSrc,
		epochDurations: make(map[string]uint64),
		spots:          make(map[string]json.RawMessage),
		marketCaches:   make(map[string]map[uint64]*candles.Cache),
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
	binCaches := make(map[uint64]*candles.Cache, len(binSizes)+1)
	s.marketCaches[mktName] = binCaches
	cacheList := make([]*candles.Cache, 0, len(binSizes)+1)
	for _, binSize := range append([]uint64{epochDur}, binSizes...) {
		cache := candles.NewCache(candles.CacheSize, binSize)
		cacheList = append(cacheList, cache)
		binCaches[binSize] = cache
		if binSize == bin5min {
			s.cache5min = cache
		}
	}
	if s.cache5min == nil {
		panic("no 5-minute cache")
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
func (s *DataAPI) ReportEpoch(base, quote uint32, epochIdx uint64, stats *matcher.MatchCycleStats) (*msgjson.Spot, error) {
	mktName, err := dex.MarketName(base, quote)
	if err != nil {
		return nil, err
	}

	// Add the candlestick.
	s.cacheMtx.Lock()
	mktCaches := s.marketCaches[mktName]
	if mktCaches == nil {
		s.cacheMtx.Unlock()
		return nil, fmt.Errorf("unknown market %q", mktName)
	}
	epochDur := s.epochDurations[mktName]
	startStamp := epochIdx * epochDur
	endStamp := startStamp + epochDur
	for _, cache := range mktCaches {
		cache.Add(&candles.Candle{
			StartStamp:  startStamp,
			EndStamp:    endStamp,
			MatchVolume: stats.MatchVolume,
			QuoteVolume: stats.QuoteVolume,
			HighRate:    stats.HighRate,
			LowRate:     stats.LowRate,
			StartRate:   stats.StartRate,
			EndRate:     stats.EndRate,
		})
	}
	change24, vol24 := s.cache5min.Delta(time.Now().Add(-time.Hour * 24))
	s.cacheMtx.Unlock()

	// Encode the spot price.
	spot := &msgjson.Spot{
		Stamp:    uint64(time.Now().UnixMilli()),
		BaseID:   base,
		QuoteID:  quote,
		Rate:     stats.EndRate,
		Change24: change24,
		Vol24:    vol24,
	}
	s.spotsMtx.Lock()
	s.spots[mktName], err = json.Marshal(spot)
	s.spotsMtx.Unlock()
	return spot, err
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
		req.NumCandles = candles.DefaultCandleRequest
	} else if req.NumCandles > candles.CacheSize {
		return nil, fmt.Errorf("requested numCandles %d exceeds maximum request size %d", req.NumCandles, candles.CacheSize)
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
	for _, s := range candles.BinSizes {
		dur, err := time.ParseDuration(s)
		if err != nil {
			panic("error parsing bin size '" + s + "': " + err.Error())
		}
		binSizes = append(binSizes, uint64(dur/time.Millisecond))
	}
}
