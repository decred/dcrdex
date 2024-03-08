// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mm

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"go.etcd.io/bbolt"
)

type dexOrderEvent struct {
	ID           string                     `json:"id"`
	Rate         uint64                     `json:"rate"`
	Qty          uint64                     `json:"qty"`
	Sell         bool                       `json:"sell"`
	Transactions []*asset.WalletTransaction `json:"transactions"`
}

type cexOrderEvent struct {
	ID          string `json:"id"`
	Rate        uint64 `json:"rate"`
	Qty         uint64 `json:"qty"`
	Sell        bool   `json:"sell"`
	BaseFilled  uint64 `json:"baseFilled"`
	QuoteFilled uint64 `json:"quoteFilled"`
}

type depositEvent struct {
	Transaction *asset.WalletTransaction `json:"transaction"`
	AssetID     uint32                   `json:"assetID"`
	CEXCredit   uint64                   `json:"cexCredit"`
}

type withdrawalEvent struct {
	ID          string                   `json:"id"`
	AssetID     uint32                   `json:"assetID"`
	Transaction *asset.WalletTransaction `json:"transaction"`
	CEXDebit    uint64                   `json:"cexDebit"`
}

// MarketMakingEvent represents an action that a market making bot takes.
type MarketMakingEvent struct {
	ID         uint64 `json:"id"`
	TimeStamp  int64  `json:"timeStamp"`
	BaseDelta  int64  `json:"baseDelta"`
	QuoteDelta int64  `json:"quoteDelta"`
	BaseFees   uint64 `json:"baseFees"`
	QuoteFees  uint64 `json:"quoteFees"`
	Pending    bool   `json:"pending"`

	// Only one of the following will be populated.
	DEXOrderEvent   *dexOrderEvent   `json:"dexOrderEvent,omitempty"`
	CEXOrderEvent   *cexOrderEvent   `json:"cexOrderEvent,omitempty"`
	DepositEvent    *depositEvent    `json:"depositEvent,omitempty"`
	WithdrawalEvent *withdrawalEvent `json:"withdrawalEvent,omitempty"`
}

// MarketMakingRun identifies a market making run.
type MarketMakingRun struct {
	StartTime int64           `json:"startTime"`
	Market    *MarketWithHost `json:"market"`
	Cfg       *BotConfig      `json:"cfg"`
}

// MarketMakingRunOverview contains information about a market making run.
type MarketMakingRunOverview struct {
	EndTime            *int64             `json:"endTime,omitempty"`
	Cfg                *BotConfig         `json:"cfg"`
	FiatRates          map[uint32]float64 `json:"fiatRates"`
	InitialDEXBalances map[uint32]uint64  `json:"initialBalances"`
	InitialCEXBalances map[uint32]uint64  `json:"initialCEXBalances"`
	BaseDelta          int64              `json:"baseDelta"`
	QuoteDelta         int64              `json:"quoteDelta"`
	BaseFees           uint64             `json:"baseFees"`
	QuoteFees          uint64             `json:"quoteFees"`
	ProfitLoss         float64            `json:"profitLoss"`
}

// eventLogDB is the interface for the event log database.
type eventLogDB interface {
	// storeNewRun stores a new run in the database. It should be called
	// whenever a new run is started. Events cannot be stored for a run
	// before this is called.
	storeNewRun(startTime int64, mkt *MarketWithHost, cfg *BotConfig, fiatRates map[uint32]float64, dexBals, cexBals map[uint32]uint64) error
	// storeEvent stores/updates a market making event.
	storeEvent(startTime int64, mkt *MarketWithHost, e *MarketMakingEvent)
	// endRun stores the time that a market making run was ended.
	endRun(startTime int64, mkt *MarketWithHost, endTime int64) error
	// updateFiatRates updates the fiat rates for a run. This should be updated
	// periodically during a run to have accurate P/L information.
	updateFiatRates(startTime int64, mkt *MarketWithHost, fiatRates map[uint32]float64) error
	// allRuns returns all runs in the database.
	allRuns() ([]*MarketMakingRun, error)
	// runOverview returns overview information about a run, not including the
	// events that took place.
	runOverview(startTime int64, mkt *MarketWithHost) (*MarketMakingRunOverview, error)
	// runEvents returns the events that took place during a run. If n == 0,
	// all of the events will be returned. If refID is not nil, the events
	// including and after the event with the ID will be returned. If
	// pendingOnly is true, only pending events will be returned.
	runEvents(startTime int64, mkt *MarketWithHost, n uint64, refID *uint64, pendingOnly bool) ([]*MarketMakingEvent, error)
}

// eventUpdate is used to asynchronously add events to the event log.
type eventUpdate struct {
	runKey []byte
	e      *MarketMakingEvent
}

type boltEventLogDB struct {
	*bbolt.DB
	log         dex.Logger
	updateEvent chan *eventUpdate
}

var _ eventLogDB = (*boltEventLogDB)(nil)

/*
 * Schema:
 *
 * - botRuns
 *   - runBucket (<startTime><baseID><quoteID><host>)
 *     - startTime
 *     - endTime
 *     - cfg
 *     - fiatRates
 *       - <assetID> -> <rate>
 *     - initialDEXBalances
 *       - <assetID> -> <balance>
 *     - initialCEXBalances
 *       - <assetID> -> <balance>
 *     - baseDelta
 *     - quoteDelta
 *     - baseFees
 *     - quoteFees
 *     - pl
 *     - events
 *       - <eventID> -> <event>
 */

var (
	botRunsBucket        = []byte("botRuns")
	eventsBucket         = []byte("events")
	fiatRatesBucket      = []byte("fiatRates")
	initialDEXBalsBucket = []byte("initialDEXBalances")
	initialCEXBalsBucket = []byte("initialCEXBalances")

	startTimeKey  = []byte("startTime")
	endTimeKey    = []byte("endTime")
	cfgKey        = []byte("cfg")
	baseDeltaKey  = []byte("baseDelta")
	quoteDeltaKey = []byte("quoteDelta")
	baseFeesKey   = []byte("baseFees")
	quoteFeesKey  = []byte("quoteFees")
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

	updateEvent := make(chan *eventUpdate, 128)
	eventLogDB := &boltEventLogDB{
		DB:          db,
		log:         log,
		updateEvent: updateEvent,
	}

	go func() {
		eventLogDB.listenForStoreEvents(ctx)
		db.Close()
	}()

	return eventLogDB, nil
}

// _updateEvent is called for each event that is popped off the updateEvent. If
// the event already exists, it is updated. If it does not exist, it is added.
// The stats for the run are also updated based on the event.
func (db *boltEventLogDB) _updateEvent(update *eventUpdate) error {
	return db.Update(func(tx *bbolt.Tx) error {
		botRuns := tx.Bucket(botRunsBucket)
		runBucket := botRuns.Bucket(update.runKey)
		if runBucket == nil {
			return fmt.Errorf("nil run bucket for key %x", update.runKey)
		}

		runBaseDelta := bToInt64(runBucket.Get(baseDeltaKey))
		runQuoteDelta := bToInt64(runBucket.Get(quoteDeltaKey))
		runBaseFees := bToUint64(runBucket.Get(baseFeesKey))
		runQuoteFees := bToUint64(runBucket.Get(quoteFeesKey))

		eventsBkt, err := runBucket.CreateBucketIfNotExists(eventsBucket)
		if err != nil {
			return err
		}

		eventKey := encode.Uint64Bytes(update.e.ID)
		oldEventB := eventsBkt.Get(eventKey)
		if oldEventB != nil {
			ver, pushes, err := encode.DecodeBlob(oldEventB)
			if err != nil {
				return err
			}
			if ver != 0 {
				return fmt.Errorf("unknown version %d", ver)
			}
			if len(pushes) != 1 {
				return fmt.Errorf("expected 1 push for event, got %d", len(pushes))
			}
			var oldEvent MarketMakingEvent
			err = json.Unmarshal(pushes[0], &oldEvent)
			if err != nil {
				return err
			}

			runBaseDelta -= oldEvent.BaseDelta
			runQuoteDelta -= oldEvent.QuoteDelta
			runBaseFees -= oldEvent.BaseFees
			runQuoteFees -= oldEvent.QuoteFees
		}

		runBaseDelta += update.e.BaseDelta
		runQuoteDelta += update.e.QuoteDelta
		runBaseFees += update.e.BaseFees
		runQuoteFees += update.e.QuoteFees

		err = runBucket.Put(baseDeltaKey, int64ToB(runBaseDelta))
		if err != nil {
			return err
		}
		err = runBucket.Put(quoteDeltaKey, int64ToB(runQuoteDelta))
		if err != nil {
			return err
		}
		err = runBucket.Put(baseFeesKey, encode.Uint64Bytes(runBaseFees))
		if err != nil {
			return err
		}
		err = runBucket.Put(quoteFeesKey, encode.Uint64Bytes(runQuoteFees))
		if err != nil {
			return err
		}

		newEventB, err := json.Marshal(update.e)
		if err != nil {
			return err
		}
		newEventB = versionedBytes(0).AddData(newEventB)

		return eventsBkt.Put(eventKey, newEventB)
	})
}

// listedForStoreEvents listens on the updateEvent channel and stores updates
// the db one at a time.
func (db *boltEventLogDB) listenForStoreEvents(ctx context.Context) {
	store := func(e *eventUpdate) {
		err := db._updateEvent(e)
		if err != nil {
			db.log.Errorf("error storing event: %v", err)
		}
	}

	for {
		select {
		case e := <-db.updateEvent:
			store(e)
		case <-ctx.Done():
			for len(db.updateEvent) > 0 {
				store(<-db.updateEvent)
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

// storeNewRun stores a new run in the database. It should be called whenever
// a new run is started. Events cannot be stored for a run before this is
// called.
func (db *boltEventLogDB) storeNewRun(startTime int64, mkt *MarketWithHost, cfg *BotConfig, fiatRates map[uint32]float64, dexBals, cexBals map[uint32]uint64) error {
	return db.Update(func(tx *bbolt.Tx) error {
		botRuns, err := tx.CreateBucketIfNotExists(botRunsBucket)
		if err != nil {
			return err
		}

		runBucket, err := botRuns.CreateBucketIfNotExists(runKey(startTime, mkt))
		if err != nil {
			return err
		}

		runBucket.Put(startTimeKey, encode.Uint64Bytes(uint64(startTime)))

		cfgB, err := json.Marshal(cfg)
		if err != nil {
			return err
		}
		runBucket.Put(cfgKey, versionedBytes(0).AddData(cfgB))

		fiatRatesBkt, err := runBucket.CreateBucketIfNotExists(fiatRatesBucket)
		if err != nil {
			return err
		}
		for assetID, rate := range fiatRates {
			fiatRatesBkt.Put(encode.Uint32Bytes(assetID), encode.Uint64Bytes(fiatToUint(rate)))
		}

		dexBalsBkt, err := runBucket.CreateBucketIfNotExists(initialDEXBalsBucket)
		if err != nil {
			return err
		}
		for assetID, bal := range dexBals {
			dexBalsBkt.Put(encode.Uint32Bytes(assetID), encode.Uint64Bytes(bal))
		}

		cexBalsBkt, err := runBucket.CreateBucketIfNotExists(initialCEXBalsBucket)
		if err != nil {
			return err
		}
		for assetID, bal := range cexBals {
			cexBalsBkt.Put(encode.Uint32Bytes(assetID), encode.Uint64Bytes(bal))
		}

		return nil
	})
}

// allRuns returns all runs in the database.
func (db *boltEventLogDB) allRuns() ([]*MarketMakingRun, error) {
	runs := make([]*MarketMakingRun, 0, 16)
	err := db.View(func(tx *bbolt.Tx) error {
		botRuns := tx.Bucket(botRunsBucket)
		return botRuns.ForEach(func(k, v []byte) error {
			startTime, mkt, err := parseRunKey(k)
			if err != nil {
				return err
			}

			runBucket := botRuns.Bucket(k)
			if runBucket == nil {
				return fmt.Errorf("nil run bucket for key %x", k)
			}

			cfg := new(BotConfig)
			cfgB := runBucket.Get(cfgKey)
			if cfgB != nil {
				ver, pushes, err := encode.DecodeBlob(cfgB)
				if err != nil {
					return err
				}
				if ver != 0 {
					return fmt.Errorf("unknown version %d", ver)
				}
				if len(pushes) != 1 {
					return fmt.Errorf("expected 1 push for cfg, got %d", len(pushes))
				}
				err = json.Unmarshal(pushes[0], cfg)
				if err != nil {
					return err
				}
			}

			runs = append(runs, &MarketMakingRun{
				StartTime: startTime,
				Market:    mkt,
				Cfg:       cfg,
			})

			return nil
		})
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(runs, func(i, j int) bool {
		return runs[i].StartTime > runs[j].StartTime
	})

	return runs, nil
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

		cfg := new(BotConfig)
		cfgB := runBucket.Get(cfgKey)
		if cfgB != nil {
			ver, pushes, err := encode.DecodeBlob(cfgB)
			if err != nil {
				return err
			}
			if ver != 0 {
				return fmt.Errorf("unknown version %d", ver)
			}
			if len(pushes) != 1 {
				return fmt.Errorf("expected 1 push for cfg, got %d", len(pushes))
			}
			err = json.Unmarshal(pushes[0], cfg)
			if err != nil {
				return err
			}
		}

		fiatRates := make(map[uint32]float64)
		fiatRatesBkt := runBucket.Bucket(fiatRatesBucket)
		if fiatRatesBkt != nil {
			err := fiatRatesBkt.ForEach(func(k, v []byte) error {
				assetID := binary.BigEndian.Uint32(k)
				rate := uintToFiat(binary.BigEndian.Uint64(v))
				fiatRates[assetID] = rate
				return nil
			})
			if err != nil {
				return err
			}
		}

		dexBals := make(map[uint32]uint64)
		dexBalsBkt := runBucket.Bucket(initialDEXBalsBucket)
		if dexBalsBkt != nil {
			err := dexBalsBkt.ForEach(func(k, v []byte) error {
				assetID := binary.BigEndian.Uint32(k)
				bal := binary.BigEndian.Uint64(v)
				dexBals[assetID] = bal
				return nil
			})
			if err != nil {
				return err
			}
		}

		cexBals := make(map[uint32]uint64)
		cexBalsBkt := runBucket.Bucket(initialCEXBalsBucket)
		if cexBalsBkt != nil {
			err := cexBalsBkt.ForEach(func(k, v []byte) error {
				assetID := binary.BigEndian.Uint32(k)
				bal := binary.BigEndian.Uint64(v)
				cexBals[assetID] = bal
				return nil
			})
			if err != nil {
				return err
			}
		}

		baseDelta := bToInt64(runBucket.Get(baseDeltaKey))
		quoteDelta := bToInt64(runBucket.Get(quoteDeltaKey))
		baseFees := bToUint64(runBucket.Get(baseFeesKey))
		quoteFees := bToUint64(runBucket.Get(quoteFeesKey))
		endTimeB := runBucket.Get(endTimeKey)
		var endTime *int64
		if endTimeB != nil {
			endTime = new(int64)
			*endTime = int64(binary.BigEndian.Uint64(endTimeB))
		}

		getFiatRate := func(assetID uint32) float64 {
			return fiatRates[assetID]
		}

		overview = &MarketMakingRunOverview{
			EndTime:            endTime,
			Cfg:                cfg,
			FiatRates:          fiatRates,
			InitialDEXBalances: dexBals,
			InitialCEXBalances: cexBals,
			BaseDelta:          baseDelta,
			QuoteDelta:         quoteDelta,
			BaseFees:           baseFees,
			QuoteFees:          quoteFees,
			ProfitLoss: calcRunProfitLoss(baseDelta, quoteDelta, baseFees, quoteFees,
				cfg.BaseID, cfg.QuoteID, getFiatRate),
		}

		return nil
	})
}

// endRun stores the time that a market making run was ended.
func (db *boltEventLogDB) endRun(startTime int64, mkt *MarketWithHost, endTime int64) error {
	return db.Update(func(tx *bbolt.Tx) error {
		botRuns := tx.Bucket(botRunsBucket)
		key := runKey(startTime, mkt)
		runBucket := botRuns.Bucket(key)
		if runBucket == nil {
			return fmt.Errorf("nil run bucket for key %x", key)
		}

		return runBucket.Put(endTimeKey, encode.Uint64Bytes(uint64(endTime)))
	})
}

// storeEvent stores/updates a market making event.
func (db *boltEventLogDB) storeEvent(startTime int64, mkt *MarketWithHost, e *MarketMakingEvent) {
	db.updateEvent <- &eventUpdate{
		runKey: runKey(startTime, mkt),
		e:      e,
	}
}

// runEvents returns the events that took place during a run. If n == 0, all of the
// events will be returned. If refID is not nil, the events including and after the
// event with the ID will be returned. If pendingOnly is true, only pending events
// will be returned.
func (db *boltEventLogDB) runEvents(startTime int64, mkt *MarketWithHost, n uint64, refID *uint64, pendingOnly bool) ([]*MarketMakingEvent, error) {
	events := make([]*MarketMakingEvent, 0, 32)
	return events, db.View(func(tx *bbolt.Tx) error {
		botRuns := tx.Bucket(botRunsBucket)
		key := runKey(startTime, mkt)
		runBucket := botRuns.Bucket(key)
		if runBucket == nil {
			return fmt.Errorf("nil run bucket for key %x", key)
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
			ver, pushes, err := encode.DecodeBlob(v)
			if err != nil {
				return err
			}
			if ver != 0 {
				return fmt.Errorf("unknown version %d", ver)
			}
			if len(pushes) != 1 {
				return fmt.Errorf("expected 1 push for event, got %d", len(pushes))
			}

			var e MarketMakingEvent
			err = json.Unmarshal(pushes[0], &e)
			if err != nil {
				return err
			}

			if pendingOnly && !e.Pending {
				continue
			}

			events = append(events, &e)
			if n > 0 && uint64(len(events)) >= n {
				break
			}
		}

		return nil
	})
}

// updateFiatRates updates the fiat rates for a run. This should be updated
// periodically during a run to have accurate P/L information.
func (db *boltEventLogDB) updateFiatRates(startTime int64, mkt *MarketWithHost, fiatRates map[uint32]float64) error {
	return db.Update(func(tx *bbolt.Tx) error {
		botRuns := tx.Bucket(botRunsBucket)
		key := runKey(startTime, mkt)
		runBucket := botRuns.Bucket(key)
		if runBucket == nil {
			return fmt.Errorf("nil run bucket for key %x", key)
		}

		fiatRatesBkt, err := runBucket.CreateBucketIfNotExists(fiatRatesBucket)
		if err != nil {
			return err
		}
		for assetID, rate := range fiatRates {
			fiatRatesBkt.Put(encode.Uint32Bytes(assetID), encode.Uint64Bytes(fiatToUint(rate)))
		}

		return nil
	})
}

func fiatToUint(f float64) uint64 {
	return uint64(math.Round(f * 100))
}

func uintToFiat(i uint64) float64 {
	return float64(i) / 100
}

func bToUint64(b []byte) uint64 {
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

func bToInt64(b []byte) int64 {
	if b == nil {
		return 0
	}
	return int64(binary.BigEndian.Uint64(b))
}

func int64ToB(i int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(i))
	return b
}
