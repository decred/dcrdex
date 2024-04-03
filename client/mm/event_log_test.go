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

	initialBals := map[uint32]uint64{
		42: 3e9,
		60: 3e9,
	}

	currBals := map[uint32]*BotBalance{
		42: {
			Available: initialBals[42],
		},
		60: {
			Available: initialBals[60],
		},
	}
	currBalanceState := func() *BalanceState {
		balances := make(map[uint32]*BotBalance, len(currBals))
		for k, v := range currBals {
			balances[k] = &BotBalance{
				Available: v.Available,
				Pending:   v.Pending,
				Locked:    v.Locked,
			}
		}
		rates := make(map[uint32]float64, len(fiatRates))
		for k, v := range fiatRates {
			rates[k] = v
		}
		return &BalanceState{
			Balances:  balances,
			FiatRates: rates,
		}
	}

	err = db.storeNewRun(startTime, mkt, cfg, currBalanceState())
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
		DEXOrderEvent: &DEXOrderEvent{
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

	currBals[42].Available += uint64(event1.BaseDelta) - event1.BaseFees
	currBals[42].Pending += 1e6
	currBals[60].Available += uint64(event1.QuoteDelta) - event1.QuoteFees
	db.storeEvent(startTime, mkt, event1, currBalanceState())

	event2 := &MarketMakingEvent{
		ID:         2,
		TimeStamp:  startTime + 1,
		BaseDelta:  1e6,
		QuoteDelta: -2e6,
		Pending:    true,
		CEXOrderEvent: &CEXOrderEvent{
			ID:          "order1",
			Rate:        5e7,
			Qty:         5e6,
			Sell:        false,
			BaseFilled:  1e6,
			QuoteFilled: 2e6,
		},
	}
	currBals[42].Available += uint64(event2.BaseDelta)
	currBals[60].Available += uint64(event2.QuoteDelta)
	currBals[42].Pending += 1e6
	db.storeEvent(startTime, mkt, event2, currBalanceState())

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

	// Update event1 and fiat rates
	event1.BaseDelta += 100
	event1.QuoteDelta -= 200
	event1.BaseFees += 20
	event1.QuoteFees += 10
	event1.Pending = false
	currBals[42].Available += 100 - 20
	currBals[60].Available -= 200 + 10
	fiatRates[42] = 25
	fiatRates[60] = 3000
	db.storeEvent(startTime, mkt, event1, currBalanceState())

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

	err = db.endRun(startTime, mkt, startTime+1000)
	if err != nil {
		t.Fatalf("error ending run: %v", err)
	}

	overview, err := db.runOverview(startTime, mkt)
	if err != nil {
		t.Fatalf("error getting run overview: %v", err)
	}
	if *overview.EndTime != startTime+1000 {
		t.Fatalf("expected end time %d, got %d", startTime+1000, overview.EndTime)
	}
	bs := currBalanceState()
	finalBals := map[uint32]uint64{
		42: bs.Balances[42].Available + bs.Balances[42].Pending + bs.Balances[42].Locked,
		60: bs.Balances[60].Available + bs.Balances[60].Pending + bs.Balances[60].Locked,
	}
	if !reflect.DeepEqual(overview.InitialBalances, initialBals) {
		t.Fatalf("expected initial balances %v, got %v", initialBals, overview.InitialBalances)
	}
	if !reflect.DeepEqual(overview.FinalBalances, finalBals) {
		t.Fatalf("expected final balances %v, got %v", finalBals, overview.FinalBalances)
	}
	expPL := calcRunProfitLoss(initialBals, finalBals, fiatRates)
	if overview.ProfitLoss != expPL {
		t.Fatalf("expected profit loss %v, got %v", expPL, overview.ProfitLoss)
	}
	if !reflect.DeepEqual(overview.Cfg, cfg) {
		t.Fatalf("expected cfg %v, got %v", cfg, overview.Cfg)
	}

	// Test sorting / pagination of runs
	err = db.storeNewRun(startTime+1, mkt, cfg, currBalanceState())
	if err != nil {
		t.Fatalf("error storing new run: %v", err)
	}
	err = db.storeNewRun(startTime-1, mkt, cfg, currBalanceState())
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
