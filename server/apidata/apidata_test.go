// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package apidata

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/matcher"
)

var dummyErr = fmt.Errorf("dummy error")

type TMarketSource struct {
	base, quote uint32
}

func (m *TMarketSource) EpochDuration() uint64 { return 1000 }
func (m *TMarketSource) Base() uint32          { return m.base }
func (m *TMarketSource) Quote() uint32         { return m.quote }

type TDBSource struct {
	loadEpochErr error
}

func (db *TDBSource) LoadEpochStats(base, quote uint32, caches []*db.CandleCache) error {
	return db.loadEpochErr
}

type TBookSource struct {
	book *msgjson.OrderBook
}

func (bs *TBookSource) Book(mktName string) (*msgjson.OrderBook, error) {
	return bs.book, nil
}

type testRig struct {
	db     *TDBSource
	api    *DataAPI
	cancel context.CancelFunc
}

func newTestRig() *testRig {
	ctx, cancel := context.WithCancel(context.Background())
	db := new(TDBSource)
	return &testRig{
		db:     db,
		api:    NewDataAPI(ctx, db),
		cancel: cancel,
	}
}

func TestAddMarketSource(t *testing.T) {
	rig := newTestRig()
	defer rig.cancel()
	// initial sucess
	err := rig.api.AddMarketSource(&TMarketSource{42, 0})
	if err != nil {
		t.Fatalf("AddMarketSource error: %v", err)
	}
	// unknown asset
	err = rig.api.AddMarketSource(&TMarketSource{42, 54321})
	if err == nil {
		t.Fatalf("no error for unknown asset")
	}
	// DB error
	rig.db.loadEpochErr = dummyErr
	err = rig.api.AddMarketSource(&TMarketSource{42, 0})
	if err == nil {
		t.Fatalf("no error for DB error")
	}
	rig.db.loadEpochErr = nil
	// success again
	err = rig.api.AddMarketSource(&TMarketSource{42, 0})
	if err != nil {
		t.Fatalf("AddMarketSource error after: %v", err)
	}
}

func TestReportEpoch(t *testing.T) {
	rig := newTestRig()
	defer rig.cancel()
	mktSrc := &TMarketSource{42, 0}
	err := rig.api.AddMarketSource(mktSrc)
	if err != nil {
		t.Fatalf("AddMarketSource error: %v", err)
	}
	epoch := encode.UnixMilliU(time.Now()) / mktSrc.EpochDuration()
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
		NumCandles: CacheSize,
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

func TestOrderBook(t *testing.T) {
	rig := newTestRig()
	defer rig.cancel()
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
