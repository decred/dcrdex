// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package candles

import (
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
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
	BinSizes = []string{"24h", "1h", "5m"}
)

// Candle is a report about the trading activity of a market over some
// specified period of time. Candles are managed with a CandleCache, which takes
// into account bin sizes and handles candle addition.
type Candle = msgjson.Candle

// Cache is a sized cache of candles. CandleCache provides methods for
// adding to the cache and reading cache data out. CandleCache is a typical
// slice until it reaches capacity, when it becomes a "circular array" to avoid
// re-allocations.
type Cache struct {
	Candles []Candle
	BinSize uint64
	cap     int
	// cursor will be the index of the last inserted candle.
	cursor int
}

// NewCache is a constructor for a Cache.
func NewCache(cap int, binSize uint64) *Cache {
	return &Cache{
		cap:     cap,
		BinSize: binSize,
	}
}

// Add adds a new candle TO THE END of the CandleCache. The caller is
// responsible to ensure that candles added with Add are always newer than
// the last candle added.
func (c *Cache) Add(candle *Candle) {
	sz := len(c.Candles)
	if sz == 0 {
		c.Candles = append(c.Candles, *candle)
		return
	}
	if c.combineCandles(c.Last(), candle) {
		return
	}
	if sz == c.cap { // circular mode
		c.cursor = (c.cursor + 1) % c.cap
		c.Candles[c.cursor] = *candle
		return
	}
	c.Candles = append(c.Candles, *candle)
	c.cursor = sz // len(c.candles) - 1
}

func (c *Cache) Reset() {
	c.cursor = 0
	c.Candles = nil
}

// WireCandles encodes up to 'count' most recent candles as
// *msgjson.WireCandles. If the CandleCache contains fewer than 'count', only
// those available will be returned, with no indication of error.
func (c *Cache) WireCandles(count int) *msgjson.WireCandles {
	n := count
	sz := len(c.Candles)
	if sz < n {
		n = sz
	}
	wc := msgjson.NewWireCandles(n)
	for i := sz - n; i < sz; i++ {
		candle := &c.Candles[(c.cursor+1+i)%sz]
		wc.StartStamps = append(wc.StartStamps, candle.StartStamp)
		wc.EndStamps = append(wc.EndStamps, candle.EndStamp)
		wc.MatchVolumes = append(wc.MatchVolumes, candle.MatchVolume)
		wc.QuoteVolumes = append(wc.QuoteVolumes, candle.QuoteVolume)
		wc.HighRates = append(wc.HighRates, candle.HighRate)
		wc.LowRates = append(wc.LowRates, candle.LowRate)
		wc.StartRates = append(wc.StartRates, candle.StartRate)
		wc.EndRates = append(wc.EndRates, candle.EndRate)
	}

	return wc
}

// Delta calculates the the change in rate, as a percentage, and total volume
// over the specified period going backwards from now. Because the first candle
// does not necessarily align with the cutoff, the rate and volume contribution
// from that candle is linearly interpreted between the endpoints. The caller is
// responsible for making sure that dur >> binSize, otherwise the results will
// be of little value.
func (c *Cache) Delta(since time.Time) (changePct float64, vol uint64) {
	cutoff := encode.UnixMilliU(since)
	sz := len(c.Candles)
	if sz == 0 {
		return 0, 0
	}
	endRate := c.Last().EndRate
	var startRate uint64
	for i := 0; i < sz; i++ {
		candle := &c.Candles[(c.cursor+sz-i)%sz]
		if candle.EndStamp <= cutoff {
			break
		} else if candle.StartStamp <= cutoff {
			// Interpret the point linearly between the start and end stamps
			cut := float64(cutoff-candle.StartStamp) / float64(candle.EndStamp-candle.StartStamp)
			rateDelta := candle.EndRate - candle.StartRate
			startRate = candle.StartRate + uint64(cut*float64(rateDelta))
			vol += uint64((1 - cut) * float64(candle.MatchVolume))

			break
		}
		startRate = candle.StartRate
		vol += candle.MatchVolume

	}
	if startRate == 0 {
		return 0, vol
	}
	return (float64(endRate) - float64(startRate)) / float64(startRate), vol
}

// last gets the most recent candle in the cache.
func (c *Cache) Last() *Candle {
	return &c.Candles[c.cursor]
}

// combineCandles attempts to add the candidate candle to the target candle
// in-place, if they're in the same bin, otherwise returns false.
func (c *Cache) combineCandles(target, candidate *Candle) bool {
	if target.EndStamp/c.BinSize != candidate.EndStamp/c.BinSize {
		// The candidate candle cannot be added.
		return false
	}
	target.EndStamp = candidate.EndStamp
	target.EndRate = candidate.EndRate
	if candidate.HighRate > target.HighRate {
		target.HighRate = candidate.HighRate
	}
	if candidate.LowRate < target.LowRate || target.LowRate == 0 {
		target.LowRate = candidate.LowRate
	}
	target.MatchVolume += candidate.MatchVolume
	target.QuoteVolume += candidate.QuoteVolume
	return true
}
