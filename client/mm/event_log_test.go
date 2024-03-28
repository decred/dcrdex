// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
)

func TestEventLogDB(t *testing.T) {
	dir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := newBoltEventLogDB(ctx, filepath.Join(dir, "event_log.db"), tLogger)
	if err != nil {
		t.Fatalf("error creating event log db: %v", err)
	}

	startTime := time.Now().Unix()
	mkt := &MarketWithHost{
		Host:    "dex.com",
		BaseID:  42,
		QuoteID: 60,
	}

	fiatRates := map[uint32]float64{
		42: 20,
		60: 2500,
	}

	cfg := &BotConfig{
		Host:             "dex.com",
		BaseID:           42,
		QuoteID:          60,
		BaseBalanceType:  Percentage,
		BaseBalance:      50,
		QuoteBalanceType: Percentage,
		QuoteBalance:     50,
		CEXCfg: &BotCEXCfg{
			Name:             "Binance",
			BaseBalanceType:  Percentage,
			BaseBalance:      50,
			QuoteBalanceType: Percentage,
			QuoteBalance:     50,
		},
		ArbMarketMakerConfig: &ArbMarketMakerConfig{
			BuyPlacements: []*ArbMarketMakingPlacement{
				{
					Lots:       1,
					Multiplier: 2,
				},
			},
			SellPlacements: []*ArbMarketMakingPlacement{
				{
					Lots:       1,
					Multiplier: 2,
				},
			},
		},
	}

	dexBals := map[uint32]uint64{
		42: 100000000,
		60: 100000000,
	}

	cexBals := map[uint32]uint64{
		42: 200000000,
		60: 200000000,
	}

	err = db.storeNewRun(startTime, mkt, cfg, fiatRates, dexBals, cexBals)
	if err != nil {
		t.Fatalf("error storing new run: %v", err)
	}

	runs, err := db.runs(0, nil, nil)
	if err != nil {
		t.Fatalf("error getting all runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}

	expectedRun := &MarketMakingRun{
		StartTime: startTime,
		Market:    mkt,
		Cfg:       cfg,
	}
	if !reflect.DeepEqual(runs[0], expectedRun) {
		t.Fatalf("expected run:\n%v\n\ngot:\n%v", expectedRun, runs[0])
	}

	event1 := &MarketMakingEvent{
		ID:         1,
		TimeStamp:  startTime + 1,
		BaseDelta:  1e6,
		QuoteDelta: -2e6,
		BaseFees:   200,
		QuoteFees:  100,
		Pending:    true,
		DEXOrderEvent: &dexOrderEvent{
			ID:   "order1",
			Rate: 5e7,
			Qty:  5e6,
			Sell: false,
			Transactions: []*asset.WalletTransaction{
				{
					Type:   asset.Swap,
					ID:     "tx1",
					Amount: 2e6,
					Fees:   100,
				},
				{
					Type:   asset.Redeem,
					ID:     "tx2",
					Amount: 1e6,
					Fees:   200,
				},
			},
		},
	}

	event2 := &MarketMakingEvent{
		ID:         2,
		TimeStamp:  startTime + 1,
		BaseDelta:  1e6,
		QuoteDelta: -2e6,
		Pending:    true,
		CEXOrderEvent: &cexOrderEvent{
			ID:          "order1",
			Rate:        5e7,
			Qty:         5e6,
			Sell:        false,
			BaseFilled:  1e6,
			QuoteFilled: 2e6,
		},
	}

	db.storeEvent(startTime, mkt, event1)
	db.storeEvent(startTime, mkt, event2)

	time.Sleep(200 * time.Millisecond)

	// Get all run events
	runEvents, err := db.runEvents(startTime, mkt, 0, nil, false)
	if err != nil {
		t.Fatalf("error getting run events: %v", err)
	}
	if len(runEvents) != 2 {
		t.Fatalf("expected 1 run event, got %d", len(runEvents))
	}
	if !reflect.DeepEqual(runEvents[0], event2) {
		t.Fatalf("expected event:\n%v\n\ngot:\n%v", event2, runEvents[0])
	}
	if !reflect.DeepEqual(runEvents[1], event1) {
		t.Fatalf("expected event:\n%v\n\ngot:\n%v", event1, runEvents[1])
	}

	// Get only 1 run event
	runEvents, err = db.runEvents(startTime, mkt, 1, nil, false)
	if err != nil {
		t.Fatalf("error getting run events: %v", err)
	}
	if len(runEvents) != 1 {
		t.Fatalf("expected 1 run event, got %d", len(runEvents))
	}
	if !reflect.DeepEqual(runEvents[0], event2) {
		t.Fatalf("expected event:\n%v\n\ngot:\n%v", event2, runEvents[0])
	}

	// Get run events with ref ID
	runEvents, err = db.runEvents(startTime, mkt, 1, &event1.ID, false)
	if err != nil {
		t.Fatalf("error getting run events: %v", err)
	}
	if len(runEvents) != 1 {
		t.Fatalf("expected 1 run event, got %d", len(runEvents))
	}
	if !reflect.DeepEqual(runEvents[0], event1) {
		t.Fatalf("expected event:\n%v\n\ngot:\n%v", event1, runEvents[0])
	}

	runs, err = db.runs(0, nil, nil)
	if err != nil {
		t.Fatalf("error getting all runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if !reflect.DeepEqual(runs[0], expectedRun) {
		t.Fatalf("expected run:\n%v\n\ngot:\n%v", expectedRun, runs[0])
	}

	// Update event1
	event1.BaseDelta += 100
	event1.QuoteDelta -= 200
	event1.BaseFees += 20
	event1.QuoteFees += 10
	event1.Pending = false
	db.storeEvent(startTime, mkt, event1)

	time.Sleep(200 * time.Millisecond)

	// Get all run events
	runEvents, err = db.runEvents(startTime, mkt, 0, nil, false)
	if err != nil {
		t.Fatalf("error getting run events: %v", err)
	}
	if len(runEvents) != 2 {
		t.Fatalf("expected 2 run events, got %d", len(runEvents))
	}
	if !reflect.DeepEqual(runEvents[0], event2) {
		t.Fatalf("expected event:\n%v\n\ngot:\n%v", event2, runEvents[0])
	}
	if !reflect.DeepEqual(runEvents[1], event1) {
		t.Fatalf("expected event:\n%v\n\ngot:\n%v", event1, runEvents[1])
	}

	runs, err = db.runs(0, nil, nil)
	if err != nil {
		t.Fatalf("error getting all runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if !reflect.DeepEqual(runs[0], expectedRun) {
		t.Fatalf("expected run:\n%v\n\ngot:\n%v", expectedRun, runs[0])
	}

	// Fetch pending runs only
	runEvents, err = db.runEvents(startTime, mkt, 0, nil, true)
	if err != nil {
		t.Fatalf("error getting run events: %v", err)
	}
	if len(runEvents) != 1 {
		t.Fatalf("expected 1 run events, got %d", len(runEvents))
	}
	if !reflect.DeepEqual(runEvents[0], event2) {
		t.Fatalf("expected event:\n%v\n\ngot:\n%v", event2, runEvents[0])
	}

	fiatRates[42] = 25
	fiatRates[60] = 3000
	err = db.updateFiatRates(startTime, mkt, fiatRates)
	if err != nil {
		t.Fatalf("error updating fiat rates: %v", err)
	}
	err = db.endRun(startTime, mkt, startTime+1000)
	if err != nil {
		t.Fatalf("error ending run: %v", err)
	}

	overview, err := db.runOverview(startTime, mkt)
	if err != nil {
		t.Fatalf("error getting run overview: %v", err)
	}
	if overview.BaseDelta != event1.BaseDelta+event2.BaseDelta {
		t.Fatalf("expected base delta %d, got %d", event1.BaseDelta+event2.BaseDelta, overview.BaseDelta)
	}
	if overview.QuoteDelta != event1.QuoteDelta+event2.QuoteDelta {
		t.Fatalf("expected quote delta %d, got %d", event1.QuoteDelta+event2.QuoteDelta, overview.QuoteDelta)
	}
	if overview.BaseFees != event1.BaseFees+event2.BaseFees {
		t.Fatalf("expected base fees %d, got %d", event1.BaseFees+event2.BaseFees, overview.BaseFees)
	}
	if overview.QuoteFees != event1.QuoteFees+event2.QuoteFees {
		t.Fatalf("expected quote fees %d, got %d", event1.QuoteFees+event2.QuoteFees, overview.QuoteFees)
	}
	if !reflect.DeepEqual(overview.FiatRates, fiatRates) {
		t.Fatalf("expected fiat rates:\n%v\n\ngot:\n%v", fiatRates, overview.FiatRates)
	}
	if *overview.EndTime != startTime+1000 {
		t.Fatalf("expected end time %d, got %d", startTime+1000, overview.EndTime)
	}
	getFiatRate := func(assetID uint32) float64 {
		return fiatRates[assetID]
	}
	expPL := calcRunProfitLoss(overview.BaseDelta, overview.QuoteDelta, overview.BaseFees, overview.QuoteFees, mkt.BaseID, mkt.QuoteID, getFiatRate)
	if overview.ProfitLoss != expPL {
		t.Fatalf("expected profit loss %v, got %v", expPL, overview.ProfitLoss)
	}

	err = db.storeNewRun(startTime+1, mkt, cfg, fiatRates, dexBals, cexBals)
	if err != nil {
		t.Fatalf("error storing new run: %v", err)
	}
	err = db.storeNewRun(startTime-1, mkt, cfg, fiatRates, dexBals, cexBals)
	if err != nil {
		t.Fatalf("error storing new run: %v", err)
	}
	runs, err = db.runs(2, nil, nil)
	if err != nil {
		t.Fatalf("error getting all runs: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	if runs[0].StartTime != startTime+1 {
		t.Fatalf("expected run start time %d, got %d", startTime+1, runs[0].StartTime)
	}
	if runs[1].StartTime != startTime {
		t.Fatalf("expected run start time %d, got %d", startTime, runs[1].StartTime)
	}

	refStartTime := uint64(startTime)
	runs, err = db.runs(2, &refStartTime, mkt)
	if err != nil {
		t.Fatalf("error getting all runs: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	if runs[0].StartTime != startTime {
		t.Fatalf("expected run start time %d, got %d", startTime, runs[0].StartTime)
	}
	if runs[1].StartTime != startTime-1 {
		t.Fatalf("expected run start time %d, got %d", startTime-1, runs[1].StartTime)
	}
}
