// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package apidata

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/matcher"
)

var dummyErr = fmt.Errorf("dummy error")

type TMarketSource struct {
	base, quote uint32
	epochDur    uint64
}

func (m *TMarketSource) EpochDuration() uint64 { return m.epochDur }
func (m *TMarketSource) Base() uint32          { return m.base }
func (m *TMarketSource) Quote() uint32         { return m.quote }

type TDBSource struct {
	loadEpochErr    error
	loadEpochCalls  int
	lastCandleCalls int
	lastCandleStamp uint64
	insertErr       error
	insertedCandles map[uint64][]candles.Candle
}

func (db *TDBSource) LoadEpochStats(base, quote uint32, caches []*candles.Cache) error {
	db.loadEpochCalls++
	return db.loadEpochErr
}

func (db *TDBSource) LastCandleEndStamp(base, quote uint32, candleDur uint64) (uint64, error) {
	db.lastCandleCalls++
	return db.lastCandleStamp, nil
}

func (db *TDBSource) InsertCandles(base, quote uint32, dur uint64, cs []*candles.Candle) error {
	if db.insertErr != nil {
		return db.insertErr
	}
	if db.insertedCandles == nil {
		db.insertedCandles = make(map[uint64][]candles.Candle)
	}
	for _, candle := range cs {
		db.insertedCandles[dur] = append(db.insertedCandles[dur], *candle)
	}
	return nil
}

type TBookSource struct {
	book *msgjson.OrderBook
}

func (bs *TBookSource) Book(mktName string) (*msgjson.OrderBook, error) {
	return bs.book, nil
}

type testRig struct {
	db  *TDBSource
	api *DataAPI
}

func newTestRig() *testRig {
	db := new(TDBSource)
	return &testRig{
		db:  db,
		api: NewDataAPI(db, func(route string, handler comms.HTTPHandler) {}),
	}
}

func TestAddMarketSource(t *testing.T) {
	rig := newTestRig()
	mkt := &TMarketSource{base: 42, quote: 0, epochDur: 1000}
	if err := rig.api.AddMarketSource(mkt); err != nil {
		t.Fatal(err)
	}
	if err := rig.api.AddMarketSource(&TMarketSource{base: 42, quote: 54321, epochDur: 1000}); err == nil {
		t.Fatal("no error for unknown asset")
	}
	if len(rig.api.marketSources) != 1 || rig.api.marketSources["dcr_btc"] != mkt {
		t.Fatal("expected only the valid market to be registered")
	}
	if rig.db.loadEpochCalls != 0 || rig.db.lastCandleCalls != 0 {
		t.Fatal("market registration read from the database")
	}
}

func TestLoadCaches(t *testing.T) {
	rig := newTestRig()
	mkt := &TMarketSource{base: 42, quote: 0, epochDur: 1000}
	if err := rig.api.AddMarketSource(mkt); err != nil {
		t.Fatal(err)
	}

	// Restoring market state can change the duration after registration.
	mkt.epochDur = 2000
	rig.db.loadEpochErr = dummyErr
	if err := rig.api.LoadCaches(); !errors.Is(err, dummyErr) {
		t.Fatalf("LoadCaches error = %v, want %v", err, dummyErr)
	}
	if len(rig.api.marketCaches) != 0 || len(rig.api.epochDurations) != 0 {
		t.Fatal("failed load published caches or epoch durations")
	}

	rig.db.loadEpochErr = nil
	if err := rig.api.LoadCaches(); err != nil {
		t.Fatal(err)
	}
	if rig.db.loadEpochCalls != 2 || rig.db.lastCandleCalls == 0 {
		t.Fatalf("database reads: LoadEpochStats %d, LastCandleEndStamp %d",
			rig.db.loadEpochCalls, rig.db.lastCandleCalls)
	}
	caches := rig.api.marketCaches["dcr_btc"]
	if len(caches) != len(binSizes)+1 {
		t.Fatalf("cache count = %d, want %d", len(caches), len(binSizes)+1)
	}
	if cache := caches[2000]; cache == nil || cache.BinSize != 2000 {
		t.Fatal("missing cache for the restored epoch duration")
	}
	if _, found := caches[1000]; found {
		t.Fatal("cache uses the duration from before restoration")
	}
	if dur := rig.api.epochDurations["dcr_btc"]; dur != 2000 {
		t.Fatalf("epoch duration = %d, want 2000", dur)
	}
}

func TestReportEpoch(t *testing.T) {
	rig := newTestRig()
	mktSrc := &TMarketSource{base: 42, quote: 0, epochDur: 1000}
	err := rig.api.AddMarketSource(mktSrc)
	if err != nil {
		t.Fatalf("AddMarketSource error: %v", err)
	}
	if err := rig.api.LoadCaches(); err != nil {
		t.Fatalf("LoadCaches error: %v", err)
	}
	epoch := uint64(time.Now().UnixMilli()) / mktSrc.EpochDuration()
	epochsPerDay := uint64(time.Hour*24/time.Millisecond) / mktSrc.EpochDuration()
	epochYesterday := epoch - epochsPerDay - 1
	// initial success
	stats := &matcher.MatchCycleStats{
		MatchVolume: 123,
		QuoteVolume: 2,
		HighRate:    10,
		LowRate:     1,
		StartRate:   4,
		EndRate:     5,
	}
	spot, err := rig.api.ReportEpoch(42, 0, epochYesterday, stats)
	if err != nil {
		t.Fatalf("ReportEpoch yesterday error: %v", err)
	}
	if spot.Vol24 != 0 {
		t.Fatalf("wrong spot Vol24. wanted 0, got %d", spot.Vol24)
	}

	spot, err = rig.api.ReportEpoch(42, 0, epoch-1, stats)
	if err != nil {
		t.Fatalf("ReportEpoch last epoch error: %v", err)
	}
	if spot.Vol24 != 123 {
		t.Fatalf("wrong spot Vol24. wanted 123, got %d", spot.Vol24)
	}

	stats.EndRate = 555

	spot, err = rig.api.ReportEpoch(42, 0, epoch, stats)
	if err != nil {
		t.Fatalf("ReportEpoch error: %v", err)
	}
	if spot.Vol24 != 246 {
		t.Fatalf("wrong spot Vol24. wanted 246, got %d", spot.Vol24)
	}
	if spot.Rate != 555 {
		t.Fatalf("wrong spot Rate. wanted 555, got %d", spot.Rate)
	}

	// handleSpots should return one spot for the one market.
	spotsI, err := rig.api.handleSpots(nil)
	if err != nil {
		t.Fatalf("handleSpots error: %v", err)
	}
	spotsEnc := spotsI.([]json.RawMessage)
	if spotsEnc == nil {
		t.Fatalf("failed to decode []json.RawMessage. spotsI is %T", spotsI)
	}
	if len(spotsEnc) != 1 {
		t.Fatalf("expected 1 spot, got %d", len(spotsEnc))
	}
	reSpot := new(msgjson.Spot)
	err = json.Unmarshal(spotsEnc[0], reSpot)
	if err != nil {
		t.Fatalf("error encoding spot: %v", err)
	}
	if reSpot.Vol24 != spot.Vol24 {
		t.Fatalf("reSpot Vol24 mismatch, wanted %d, got %d", spot.Vol24, reSpot.Vol24)
	}

	// handleCandles should return three candles.
	candlesI, err := rig.api.handleCandles(&msgjson.CandlesRequest{
		BaseID:     42,
		QuoteID:    0,
		BinSize:    "1s", // Epoch duration, smallest candle size
		NumCandles: candles.CacheSize,
	})
	if err != nil {
		t.Fatalf("handleCandles error: %v", err)
	}
	wireCandles := candlesI.(*msgjson.WireCandles)
	if wireCandles == nil {
		t.Fatalf("failed to decode *msgjson.WireCandles. candlesI is %T", candlesI)
	}
	if len(wireCandles.StartRates) != 3 {
		t.Fatalf("wrong number of candles. expected 3, got %d", len(wireCandles.StartRates))
	}
}

func TestReportEpochDurationChange(t *testing.T) {
	const fiveMinutes = uint64(5 * time.Minute / time.Millisecond)
	for _, tc := range []struct {
		name           string
		oldDur, newDur uint64
		keepOldCache   bool
		insertErr      error
	}{
		{name: "new epoch interval", oldDur: 5000, newDur: 10000},
		{name: "retain old standard interval", oldDur: fiveMinutes, newDur: 10000, keepOldCache: true},
		{name: "reuse new standard interval", oldDur: 10000, newDur: fiveMinutes},
		{name: "unchanged duration", oldDur: 10000, newDur: 10000, keepOldCache: true},
		{name: "storage failure after changing duration", oldDur: 10000, newDur: fiveMinutes, insertErr: dummyErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Keep the reports in separate bins for every standard interval.
			boundary := uint64(time.Now().UTC().Truncate(24 * time.Hour).UnixMilli())
			rig := newTestRig()
			rig.db.lastCandleStamp = boundary - uint64(48*time.Hour/time.Millisecond)
			mkt := &TMarketSource{base: 42, quote: 0, epochDur: tc.oldDur}
			if err := rig.api.AddMarketSource(mkt); err != nil {
				t.Fatal(err)
			}
			if err := rig.api.LoadCaches(); err != nil {
				t.Fatal(err)
			}

			if _, err := rig.api.ReportEpoch(42, 0, boundary/tc.oldDur-2,
				&matcher.MatchCycleStats{MatchVolume: 7, EndRate: 100}); err != nil {
				t.Fatal(err)
			}
			caches := rig.api.marketCaches["dcr_btc"]
			oldEpochCache := caches[tc.oldDur]
			standardCaches := make(map[uint64]*cacheWithStoredTime, len(binSizes))
			for _, dur := range binSizes {
				standardCaches[dur] = caches[dur]
			}

			mkt.epochDur = tc.newDur
			rig.db.insertErr = tc.insertErr
			_, err := rig.api.ReportEpoch(42, 0, boundary/tc.newDur,
				&matcher.MatchCycleStats{MatchVolume: 13, EndRate: 200})
			if !errors.Is(err, tc.insertErr) {
				t.Fatalf("ReportEpoch error = %v, want %v", err, tc.insertErr)
			}
			if tc.insertErr != nil {
				for dur, cache := range standardCaches {
					if cache.lastStoredEndStamp != rig.db.lastCandleStamp {
						t.Fatalf("failed insert advanced stored timestamp for interval %d", dur)
					}
				}
				return
			}
			if dur := rig.api.epochDurations["dcr_btc"]; dur != tc.newDur {
				t.Fatalf("cached epoch duration = %d, want %d", dur, tc.newDur)
			}
			if tc.keepOldCache {
				if caches[tc.oldDur] != oldEpochCache {
					t.Fatal("old cache was replaced")
				}
			} else if _, found := caches[tc.oldDur]; found {
				t.Fatal("old epoch cache was not removed")
			}
			newCache := caches[tc.newDur]
			if newCache == nil || newCache.BinSize != tc.newDur || len(newCache.Candles) == 0 {
				t.Fatal("missing candle for the current epoch duration")
			}
			if tc.oldDur == tc.newDur && (len(newCache.Candles) != 2 || newCache.Candles[0].MatchVolume != 7) {
				t.Fatal("unchanged epoch cache lost its history")
			}
			last := newCache.Last()
			if last.StartStamp != boundary || last.EndStamp != boundary+tc.newDur || last.MatchVolume != 13 {
				t.Fatalf("new epoch candle = %+v, want stamps %d/%d and volume 13",
					last, boundary, boundary+tc.newDur)
			}
			if len(rig.db.insertedCandles) != len(binSizes) {
				t.Fatalf("stored %d candle intervals, want %d", len(rig.db.insertedCandles), len(binSizes))
			}
			for dur, original := range standardCaches {
				cache := caches[dur]
				if cache != original {
					t.Fatalf("standard cache %d was replaced", dur)
				}
				stored := rig.db.insertedCandles[dur]
				if len(stored) != 1 || stored[0].MatchVolume != 7 || stored[0].EndStamp != boundary-tc.oldDur {
					t.Fatalf("completed candles stored for interval %d = %+v", dur, stored)
				}
				if cache.lastStoredEndStamp != stored[0].EndStamp {
					t.Fatalf("stored timestamp for interval %d = %d, want %d", dur,
						cache.lastStoredEndStamp, stored[0].EndStamp)
				}
				cs := cache.CandlesCopy()
				if len(cs) != 2 || cs[0].MatchVolume != 7 || cs[1].MatchVolume != 13 {
					t.Fatalf("standard cache %d lost candle history: %+v", dur, cs)
				}
			}
		})
	}
}

func TestOrderBook(t *testing.T) {
	rig := newTestRig()
	book := new(msgjson.OrderBook)
	rig.api.SetBookSource(&TBookSource{book})
	bookI, err := rig.api.handleOrderBook(&msgjson.OrderBookSubscription{
		Base:  42,
		Quote: 0,
	})
	if err != nil {
		t.Fatalf("handleOrderBook error: %v", err)
	}
	reBook := bookI.(*msgjson.OrderBook)
	if reBook == nil {
		t.Fatalf("failed to decode *msgjson.OrderBook. bookI is %T", bookI)
	}
	if reBook != book {
		t.Fatalf("where did this book come from?")
	}
}
