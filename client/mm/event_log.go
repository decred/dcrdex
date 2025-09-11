// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"go.etcd.io/bbolt"
)

// DEXOrderEvent represents a a dex order that a bot placed.
type DEXOrderEvent struct {
	ID           string                     `json:"id"`
	Rate         uint64                     `json:"rate"`
	Qty          uint64                     `json:"qty"`
	Sell         bool                       `json:"sell"`
	Transactions []*asset.WalletTransaction `json:"transactions"`
}

// CEXOrderEvent represents a a cex order that a bot placed.
type CEXOrderEvent struct {
	ID      string `json:"id"`
	BaseID  uint32 `json:"baseID"`
	QuoteID uint32 `json:"quoteID"`
	// Qty is always in units of the base asset, except for market buys.
	Qty         uint64 `json:"qty"`
	Rate        uint64 `json:"rate"`
	Market      bool   `json:"market"`
	Sell        bool   `json:"sell"`
	BaseFilled  uint64 `json:"baseFilled"`
	QuoteFilled uint64 `json:"quoteFilled"`
}

// DepositEvent represents a deposit that a bot made.
type DepositEvent struct {
	DepositTx  *asset.WalletTransaction `json:"transaction,omitempty"`
	BridgeTx   *asset.WalletTransaction `json:"bridgeTx,omitempty"`
	DexAssetID uint32                   `json:"assetID"`
	CEXAssetID uint32                   `json:"cexAssetID"`
	CEXCredit  uint64                   `json:"cexCredit"`
}

// WithdrawalEvent represents a withdrawal that a bot made.
type WithdrawalEvent struct {
	ID           string                   `json:"id"`
	DEXAssetID   uint32                   `json:"assetID"`
	CEXAssetID   uint32                   `json:"cexAssetID"`
	WithdrawalTx *asset.WalletTransaction `json:"transaction"`
	BridgeTx     *asset.WalletTransaction `json:"bridgeTx,omitempty"`
	CEXDebit     uint64                   `json:"cexDebit"`
}

// MarketMakingEvent represents an action that a market making bot takes.
type MarketMakingEvent struct {
	ID             uint64          `json:"id"`
	TimeStamp      int64           `json:"timestamp"`
	Pending        bool            `json:"pending"`
	BalanceEffects *BalanceEffects `json:"balanceEffects,omitempty"`

	// Only one of the following will be populated.
	DEXOrderEvent   *DEXOrderEvent    `json:"dexOrderEvent,omitempty"`
	CEXOrderEvent   *CEXOrderEvent    `json:"cexOrderEvent,omitempty"`
	DepositEvent    *DepositEvent     `json:"depositEvent,omitempty"`
	WithdrawalEvent *WithdrawalEvent  `json:"withdrawalEvent,omitempty"`
	UpdateConfig    *BotConfig        `json:"updateConfig,omitempty"`
	UpdateInventory *map[uint32]int64 `json:"updateInventory,omitempty"`
}

// MarketMakingRun identifies a market making run.
type MarketMakingRun struct {
	StartTime int64           `json:"startTime"`
	Market    *MarketWithHost `json:"market"`
	Profit    float64         `json:"profit"`
}

// BalanceState contains the fiat rates and bot balances at the latest point of
// a run.
type BalanceState struct {
	FiatRates     map[uint32]float64     `json:"fiatRates"`
	Balances      map[uint32]*BotBalance `json:"balances"`
	InventoryMods map[uint32]int64       `json:"invMods"`
}

// CfgUpdate contains a BotConfig and the timestamp the bot started to use this
// configuration.
type CfgUpdate struct {
	Timestamp int64      `json:"timestamp"`
	Cfg       *BotConfig `json:"cfg"`
}

// MarketMakingRunOverview contains information about a market making run.
type MarketMakingRunOverview struct {
	EndTime         *int64            `json:"endTime,omitempty"`
	Cfgs            []*CfgUpdate      `json:"cfgs"`
	InitialBalances map[uint32]uint64 `json:"initialBalances"`
	ProfitLoss      *ProfitLoss       `json:"profitLoss"`
	FinalState      *BalanceState     `json:"finalState"`
}

// eventLogDB is the interface for the event log database.
type eventLogDB interface {
	// storeNewRun stores a new run in the database. It should be called
	// whenever a new run is started. Events cannot be stored for a run
	// before this is called.
	storeNewRun(startTime int64, mkt *MarketWithHost, cfg *BotConfig, initialState *BalanceState) error
	// storeEvent stores/updates a market making event.
	storeEvent(startTime int64, mkt *MarketWithHost, e *MarketMakingEvent, fs *BalanceState)
	// endRun stores the time that a market making run was ended.
	endRun(startTime int64, mkt *MarketWithHost) error
	// runs returns a list of runs in the database. If n == 0, all of the runs
	// will be returned. If refStartTime and refMkt are not nil, the runs
	// including and before the run with the start time and market will be
	// returned.
	runs(n uint64, refStartTime *uint64, refMkt *MarketWithHost) ([]*MarketMakingRun, error)
	// runOverview returns overview information about a run, not including the
	// events that took place.
	runOverview(startTime int64, mkt *MarketWithHost) (*MarketMakingRunOverview, error)
	// runEvents returns the events that took place during a run. If n == 0,
	// all of the events will be returned. If refID is not nil, the events
	// including and after the event with the ID will be returned. If
	// pendingOnly is true, only pending events will be returned.
	runEvents(startTime int64, mkt *MarketWithHost, n uint64, refID *uint64, pendingOnly bool, filters *RunLogFilters) ([]*MarketMakingEvent, error)
}

// eventUpdate is used to asynchronously add events to the event log.
type eventUpdate struct {
	runKey []byte
	e      *MarketMakingEvent
	bs     *BalanceState
}

type boltEventLogDB struct {
	*bbolt.DB
	log          dex.Logger
	eventUpdates chan *eventUpdate
}

var _ eventLogDB = (*boltEventLogDB)(nil)

/*
 * Schema:
 *
 * - botRuns
 *   - version
 *   - runBucket (<startTime><baseID><quoteID><host>)
 *     - startTime
 *     - endTime
 *     - cfg
 *     - ib
 *     - fs
 *     - cfgs
 *       - <timestamp> -> <cfg>
 *     - events
 *       - <eventID> -> <event>
 */

var (
	botRunsBucket = []byte("botRuns")
	versionKey    = []byte("version")
	eventsBucket  = []byte("events")
	cfgsBucket    = []byte("cfgs")

	startTimeKey   = []byte("startTime")
	endTimeKey     = []byte("endTime")
	initialBalsKey = []byte("ib")
	finalStateKey  = []byte("fs")
	noPendingKey   = []byte("np")
)

func newBoltEventLogDB(ctx context.Context, path string, log dex.Logger) (*boltEventLogDB, error) {
	db, err := bbolt.Open(path, 0600, nil)
	if err != nil {
		return nil, err
	}

	err = db.Update(func(tx *bbolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(botRunsBucket)
		return err
	})
	if err != nil {
		return nil, err
	}

	eventUpdates := make(chan *eventUpdate, 128)
	eventLogDB := &boltEventLogDB{
		DB:           db,
		log:          log,
		eventUpdates: eventUpdates,
	}

	err = eventLogDB.upgradeDB()
	if err != nil {
		return nil, err
	}

	go func() {
		eventLogDB.listenForStoreEvents(ctx)
		db.Close()
	}()

	return eventLogDB, nil
}

// calcFinalStateBasedOnEventDiff calculated an updated final balance state
// based on the difference between an existing event and its updated version.
func calcFinalStateBasedOnEventDiff(runBucket, eventsBucket *bbolt.Bucket, eventKey []byte, newEvent *MarketMakingEvent) (*BalanceState, error) {
	finalStateB := runBucket.Get(finalStateKey)
	if finalStateB == nil {
		return nil, fmt.Errorf("no final state found")
	}

	finalState, err := decodeFinalState(finalStateB)
	if err != nil {
		return nil, err
	}

	originalEventB := eventsBucket.Get(eventKey)
	if originalEventB == nil {
		return nil, fmt.Errorf("no original event found")
	}

	originalEvent, err := decodeMarketMakingEvent(originalEventB)
	if err != nil {
		return nil, err
	}

	applyDiff := func(curr uint64, diff int64) uint64 {
		if diff > 0 {
			return curr + uint64(diff)
		}

		if curr < uint64(-diff) {
			return 0
		}

		return curr + uint64(diff)
	}

	balanceEffectDiff := newEvent.BalanceEffects.sub(originalEvent.BalanceEffects)
	for assetID, diff := range balanceEffectDiff.settled {
		finalState.Balances[assetID].Available = applyDiff(finalState.Balances[assetID].Available, diff)
	}
	for assetID, diff := range balanceEffectDiff.pending {
		finalState.Balances[assetID].Pending = applyDiff(finalState.Balances[assetID].Pending, diff)
	}
	for assetID, diff := range balanceEffectDiff.locked {
		finalState.Balances[assetID].Locked = applyDiff(finalState.Balances[assetID].Locked, diff)
	}
	for assetID, diff := range balanceEffectDiff.reserved {
		finalState.Balances[assetID].Reserved = applyDiff(finalState.Balances[assetID].Reserved, diff)
	}

	return finalState, nil
}

// updateEvent is called for each event that is popped off the updateEvent. If
// the event already exists, it is updated. If it does not exist, it is added.
// The stats for the run are also updated based on the event.
func (db *boltEventLogDB) updateEvent(update *eventUpdate) {
	if err := db.Update(func(tx *bbolt.Tx) error {
		botRuns := tx.Bucket(botRunsBucket)
		runBucket := botRuns.Bucket(update.runKey)
		if runBucket == nil {
			return fmt.Errorf("nil run bucket for key %x", update.runKey)
		}

		eventsBkt, err := runBucket.CreateBucketIfNotExists(eventsBucket)
		if err != nil {
			return err
		}
		eventKey := encode.Uint64Bytes(update.e.ID)
		eventJSON, err := json.Marshal(update.e)
		if err != nil {
			return err
		}
		eventB := versionedBytes(0).AddData(eventJSON)

		bs := update.bs
		if bs == nil {
			bs, err = calcFinalStateBasedOnEventDiff(runBucket, eventsBkt, eventKey, update.e)
			if err != nil {
				return err
			}
		}

		if err := eventsBkt.Put(eventKey, eventB); err != nil {
			return err
		}

		if update.e.UpdateConfig != nil {
			if err := db.storeCfgUpdate(runBucket, update.e.UpdateConfig, update.e.TimeStamp); err != nil {
				return err
			}
		}

		err = storeEndTime(runBucket)
		if err != nil {
			return err
		}

		// Update the final state.
		bsJSON, err := json.Marshal(bs)
		if err != nil {
			return err
		}
		return runBucket.Put(finalStateKey, versionedBytes(0).AddData(bsJSON))
	}); err != nil {
		db.log.Errorf("error storing event: %v", err)
	}
}

// listedForStoreEvents listens on the updateEvent channel and updates the
// db one at a time.
func (db *boltEventLogDB) listenForStoreEvents(ctx context.Context) {
	for {
		select {
		case e := <-db.eventUpdates:
			db.updateEvent(e)
		case <-ctx.Done():
			for len(db.eventUpdates) > 0 {
				db.updateEvent(<-db.eventUpdates)
			}
			return
		}
	}
}

func versionedBytes(v byte) encode.BuildyBytes {
	return encode.BuildyBytes{v}
}

// runKey is the key for a run bucket.
func runKey(startTime int64, mkt *MarketWithHost) []byte {
	return versionedBytes(0).
		AddData(encode.Uint64Bytes(uint64(startTime))).
		AddData(encode.Uint32Bytes(mkt.BaseID)).
		AddData(encode.Uint32Bytes(mkt.QuoteID)).
		AddData([]byte(mkt.Host))
}

func parseRunKey(key []byte) (startTime int64, mkt *MarketWithHost, err error) {
	ver, pushes, err := encode.DecodeBlob(key)
	if err != nil {
		return 0, nil, err
	}
	if ver != 0 {
		return 0, nil, fmt.Errorf("unknown version %d", ver)
	}
	if len(pushes) != 4 {
		return 0, nil, fmt.Errorf("expected 4 pushes, got %d", len(pushes))
	}

	startTime = int64(binary.BigEndian.Uint64(pushes[0]))
	mkt = &MarketWithHost{
		BaseID:  binary.BigEndian.Uint32(pushes[1]),
		QuoteID: binary.BigEndian.Uint32(pushes[2]),
		Host:    string(pushes[3]),
	}
	return
}

func (db *boltEventLogDB) Close() error {
	close(db.eventUpdates)
	return db.DB.Close()
}

func (db *boltEventLogDB) storeCfgUpdate(runBucket *bbolt.Bucket, newCfg *BotConfig, timestamp int64) error {
	cfgsBucket, err := runBucket.CreateBucketIfNotExists(cfgsBucket)
	if err != nil {
		return err
	}
	cfgB, err := json.Marshal(newCfg)
	if err != nil {
		return err
	}
	return cfgsBucket.Put(encode.Uint64Bytes(uint64(timestamp)), versionedBytes(0).AddData(cfgB))
}

func decodeBotConfig(b []byte) (*BotConfig, error) {
	ver, pushes, err := encode.DecodeBlob(b)
	if err != nil {
		return nil, err
	}
	if ver != 0 {
		return nil, fmt.Errorf("unknown version %d", ver)
	}
	if len(pushes) != 1 {
		return nil, fmt.Errorf("expected 1 push for cfg, got %d", len(pushes))
	}

	cfg := new(BotConfig)
	return cfg, json.Unmarshal(pushes[0], cfg)
}

func (db *boltEventLogDB) cfgUpdates(runBucket *bbolt.Bucket) ([]*CfgUpdate, error) {
	cfgsBucket := runBucket.Bucket(cfgsBucket)
	if cfgsBucket == nil {
		return nil, nil
	}

	cfgs := make([]*CfgUpdate, 0, cfgsBucket.Stats().KeyN)
	cursor := cfgsBucket.Cursor()
	for k, v := cursor.First(); k != nil; k, v = cursor.Next() {
		timestamp := int64(binary.BigEndian.Uint64(k))
		cfg, err := decodeBotConfig(v)
		if err != nil {
			return nil, err
		}
		cfgs = append(cfgs, &CfgUpdate{
			Timestamp: timestamp,
			Cfg:       cfg,
		})
	}

	return cfgs, nil
}

// storeNewRun stores a new run in the database. It should be called whenever
// a new run is started. Events cannot be stored for a run before this is
// called.
func (db *boltEventLogDB) storeNewRun(startTime int64, mkt *MarketWithHost, cfg *BotConfig, initialState *BalanceState) error {
	return db.Update(func(tx *bbolt.Tx) error {
		botRuns, err := tx.CreateBucketIfNotExists(botRunsBucket)
		if err != nil {
			return err
		}

		runBucket, err := botRuns.CreateBucketIfNotExists(runKey(startTime, mkt))
		if err != nil {
			return err
		}

		err = runBucket.Put(startTimeKey, encode.Uint64Bytes(uint64(startTime)))
		if err != nil {
			return err
		}

		err = storeEndTime(runBucket)
		if err != nil {
			return err
		}

		if err := db.storeCfgUpdate(runBucket, cfg, startTime); err != nil {
			return err
		}

		initialBals := make(map[uint32]uint64)
		for assetID, bal := range initialState.Balances {
			initialBals[assetID] = bal.Available
		}
		initialBalsB, err := json.Marshal(initialBals)
		if err != nil {
			return err
		}
		runBucket.Put(initialBalsKey, versionedBytes(0).AddData(initialBalsB))

		fsB, err := json.Marshal(initialState)
		if err != nil {
			return err
		}
		runBucket.Put(finalStateKey, versionedBytes(0).AddData(fsB))

		return nil
	})
}

// runs returns a list of runs in the database. If n == 0, all of the runs will
// be returned. If refStartTime and refMkt are not nil, the runs including and
// before the run with the start time and market will be returned.
func (db *boltEventLogDB) runs(n uint64, refStartTime *uint64, refMkt *MarketWithHost) ([]*MarketMakingRun, error) {
	if refStartTime == nil != (refMkt == nil) {
		return nil, fmt.Errorf("both or neither refStartTime and refMkt must be nil")
	}

	var runs []*MarketMakingRun
	err := db.View(func(tx *bbolt.Tx) error {
		botRuns := tx.Bucket(botRunsBucket)

		runs = make([]*MarketMakingRun, 0, botRuns.Stats().BucketN)
		cursor := botRuns.Cursor()

		var k []byte
		if refStartTime == nil {
			k, _ = cursor.Last()
		} else {
			k, _ = cursor.Seek(runKey(int64(*refStartTime), refMkt))
		}

		for ; k != nil; k, _ = cursor.Prev() {
			if bytes.Equal(k, versionKey) {
				continue
			}
			startTime, mkt, err := parseRunKey(k)
			if err != nil {
				return err
			}

			runBucket := botRuns.Bucket(k)
			if runBucket == nil {
				return fmt.Errorf("nil run bucket for key %x", k)
			}

			overview, err := db.queryRunOverview(runBucket, false)
			if err != nil {
				return err
			}

			runs = append(runs, &MarketMakingRun{
				StartTime: startTime,
				Market:    mkt,
				Profit:    overview.ProfitLoss.Profit,
			})

			if n > 0 && uint64(len(runs)) >= n {
				break
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return runs, nil
}

func decodeFinalState(finalStateB []byte) (*BalanceState, error) {
	finalState := new(BalanceState)
	ver, pushes, err := encode.DecodeBlob(finalStateB)
	if err != nil {
		return nil, err
	}
	if ver != 0 {
		return nil, fmt.Errorf("unknown final state version %d", ver)
	}
	if len(pushes) != 1 {
		return nil, fmt.Errorf("expected 1 push for final state, got %d", len(pushes))
	}
	err = json.Unmarshal(pushes[0], finalState)
	if err != nil {
		return nil, err
	}
	return finalState, nil
}

func (db *boltEventLogDB) queryRunOverview(runBucket *bbolt.Bucket, includeCfgs bool) (*MarketMakingRunOverview, error) {
	initialBalsB := runBucket.Get(initialBalsKey)
	initialBals := make(map[uint32]uint64)
	ver, pushes, err := encode.DecodeBlob(initialBalsB)
	if err != nil {
		return nil, err
	}
	if ver != 0 {
		return nil, fmt.Errorf("unknown initial bals version %d", ver)
	}
	if len(pushes) != 1 {
		return nil, fmt.Errorf("expected 1 push for initial bals, got %d", len(pushes))
	}
	err = json.Unmarshal(pushes[0], &initialBals)
	if err != nil {
		return nil, err
	}

	finalStateB := runBucket.Get(finalStateKey)
	if finalStateB == nil {
		return nil, fmt.Errorf("no final state found")
	}

	finalState, err := decodeFinalState(finalStateB)
	if err != nil {
		return nil, err
	}

	var cfgs []*CfgUpdate
	if includeCfgs {
		cfgs, err = db.cfgUpdates(runBucket)
		if err != nil {
			return nil, err
		}
	}

	endTimeB := runBucket.Get(endTimeKey)
	var endTime *int64
	if len(endTimeB) == 8 {
		endTime = new(int64)
		*endTime = int64(binary.BigEndian.Uint64(endTimeB))
	}

	finalBals := make(map[uint32]uint64)
	for assetID, bal := range finalState.Balances {
		finalBals[assetID] = bal.Available + bal.Pending + bal.Locked + bal.Reserved
	}

	return &MarketMakingRunOverview{
		EndTime:         endTime,
		Cfgs:            cfgs,
		InitialBalances: initialBals,
		ProfitLoss:      newProfitLoss(initialBals, finalBals, finalState.InventoryMods, finalState.FiatRates),
		FinalState:      finalState,
	}, nil
}

// runOverview returns overview information about a run, not including the
// events that took place.
func (db *boltEventLogDB) runOverview(startTime int64, mkt *MarketWithHost) (*MarketMakingRunOverview, error) {
	var overview *MarketMakingRunOverview
	return overview, db.View(func(tx *bbolt.Tx) error {
		botRuns := tx.Bucket(botRunsBucket)
		key := runKey(startTime, mkt)
		runBucket := botRuns.Bucket(key)
		if runBucket == nil {
			return fmt.Errorf("nil run bucket for key %x", key)
		}

		var err error
		overview, err = db.queryRunOverview(runBucket, true)
		if err != nil {
			return err
		}

		return nil
	})
}

// storeEndTime updates the end time of a run to the current time.
func storeEndTime(runBucket *bbolt.Bucket) error {
	return runBucket.Put(endTimeKey, encode.Uint64Bytes(uint64(time.Now().Unix())))
}

// endRun stores the time that a market making run was ended.
func (db *boltEventLogDB) endRun(startTime int64, mkt *MarketWithHost) error {
	return db.Update(func(tx *bbolt.Tx) error {
		botRuns := tx.Bucket(botRunsBucket)
		key := runKey(startTime, mkt)
		runBucket := botRuns.Bucket(key)
		if runBucket == nil {
			return fmt.Errorf("nil run bucket for key %x", key)
		}

		return storeEndTime(runBucket)
	})
}

// storeEvent stores/updates a market making event. BalanceState can be nil if
// the run has ended and the final state should be updated based on the
// difference between the event and the previous version of the event.
func (db *boltEventLogDB) storeEvent(startTime int64, mkt *MarketWithHost, e *MarketMakingEvent, bs *BalanceState) {
	db.eventUpdates <- &eventUpdate{
		runKey: runKey(startTime, mkt),
		e:      e,
		bs:     bs,
	}
}

func decodeMarketMakingEvent(eventB []byte) (*MarketMakingEvent, error) {
	e := new(MarketMakingEvent)
	ver, pushes, err := encode.DecodeBlob(eventB)
	if err != nil {
		return nil, err
	}
	if ver != 0 {
		return nil, fmt.Errorf("unknown version %d", ver)
	}
	if len(pushes) != 1 {
		return nil, fmt.Errorf("expected 1 push for event, got %d", len(pushes))
	}
	return e, json.Unmarshal(pushes[0], e)
}

// runEvents returns the events that took place during a run. If n == 0, all of the
// events will be returned. If refID is not nil, the events including and after the
// event with the ID will be returned. If pendingOnly is true, only pending events
// will be returned.
func (db *boltEventLogDB) runEvents(startTime int64, mkt *MarketWithHost, n uint64, refID *uint64, pendingOnly bool, filter *RunLogFilters) ([]*MarketMakingEvent, error) {
	events := make([]*MarketMakingEvent, 0, 32)

	return events, db.View(func(tx *bbolt.Tx) error {
		botRuns := tx.Bucket(botRunsBucket)
		key := runKey(startTime, mkt)
		runBucket := botRuns.Bucket(key)
		if runBucket == nil {
			return fmt.Errorf("nil run bucket for key %x", key)
		}

		// If a run has ended, and we have queried for all pending events and
		// nothing was found, we store this fact to avoid having to search for
		// pending events again.
		if pendingOnly {
			noPending := runBucket.Get(noPendingKey)
			if noPending != nil {
				return nil
			}
		}

		eventsBkt := runBucket.Bucket(eventsBucket)
		if eventsBkt == nil {
			return nil
		}

		cursor := eventsBkt.Cursor()
		var k, v []byte
		if refID == nil {
			k, v = cursor.Last()
		} else {
			k, v = cursor.Seek(encode.Uint64Bytes(*refID))
		}

		for ; k != nil; k, v = cursor.Prev() {
			e, err := decodeMarketMakingEvent(v)
			if err != nil {
				return err
			}

			if pendingOnly && !e.Pending {
				continue
			}

			if filter != nil && !filter.filter(e) {
				continue
			}

			events = append(events, e)
			if n > 0 && uint64(len(events)) >= n {
				break
			}
		}

		// If there are no pending events, and the run as ended, store this
		// information to avoid unnecessary iteration in the future.
		if pendingOnly && len(events) == 0 && refID == nil && n == 0 {
			endTime := runBucket.Get(endTimeKey)
			if endTime != nil {
				runBucket.Put(noPendingKey, []byte{1})
			}
		}

		return nil
	})
}
