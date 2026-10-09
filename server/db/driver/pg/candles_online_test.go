//go:build pgonline

package pg

import (
	"testing"

	"decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/server/db"
)

func TestCandles(t *testing.T) {
	if err := cleanTables(archie.db); err != nil {
		t.Fatalf("cleanTables: %v", err)
	}

	var baseID, quoteID uint32 = 42, 0
	var candleDur uint64 = 5 * 60 * 1000

	lastCandle, err := archie.LastCandleEndStamp(baseID, quoteID, candleDur)
	if err != nil {
		t.Fatalf("Initial LastCandleEndStamp error: %v", err)
	}

	cands := []*candles.Candle{
		{EndStamp: candleDur},
		{EndStamp: candleDur * 2},
	}

	if err = archie.InsertCandles(baseID, quoteID, candleDur, cands); err != nil {
		t.Fatalf("InsertCandles error: %v", err)
	}

	lastCandle, err = archie.LastCandleEndStamp(baseID, quoteID, candleDur)
	if err != nil {
		t.Fatalf("LastCandleEndStamp error: %v", err)
	}

	if lastCandle != candleDur*2 {
		t.Fatalf("Wrong last candle. Wanted 2, got %d", lastCandle)
	}

	// Updating is fine
	cands[1].MatchVolume = 1
	if err = archie.InsertCandles(baseID, quoteID, candleDur, []*candles.Candle{cands[1]}); err != nil {
		t.Fatalf("InsertCandles (overwrite) error: %v", err)
	}

	cache := candles.NewCache(5, candleDur)
	if err = archie.LoadEpochStats(baseID, quoteID, []*candles.Cache{cache}); err != nil {
		t.Fatalf("LoadEpochStats error: %v", err)
	}

	if len(cache.Candles) != 2 {
		t.Fatalf("Expected 2 candles, got %d", len(cache.Candles))
	}

	if cache.Last().MatchVolume != 1 {
		t.Fatalf("Overwrite failed")
	}

	// The newest epoch has not yet been saved as a completed candle.
	if err := archie.insertEpoch(archie.db, &db.EpochResults{
		MktBase: baseID, MktQuote: quoteID,
		Idx: 2, Dur: int64(candleDur), MatchVolume: 7,
	}); err != nil {
		t.Fatal(err)
	}
	cache = candles.NewCache(5, candleDur)
	if err := archie.LoadEpochStats(baseID, quoteID, []*candles.Cache{cache}); err != nil {
		t.Fatal(err)
	}
	if len(cache.Candles) != 3 {
		t.Fatalf("restored %d candles, want 3", len(cache.Candles))
	}
	last := cache.Last()
	if last.StartStamp != candleDur*2 || last.EndStamp != candleDur*3 || last.MatchVolume != 7 {
		t.Fatalf("latest restored candle = %+v, want stamps %d/%d and volume 7",
			last, candleDur*2, candleDur*3)
	}
}
