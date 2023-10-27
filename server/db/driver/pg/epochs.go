// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"time"

	"decred.org/dcrdex/dex/candles"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/db"
	"decred.org/dcrdex/server/db/driver/pg/internal"
	"github.com/lib/pq"
)

// In a table, a []order.OrderID is stored as a BYTEA[]. The orderIDs type
// defines the Value and Scan methods for such an OrderID slice using
// pq.ByteaArray and copying of OrderId data to/from []byte.
type orderIDs []order.OrderID

// Value implements the sql/driver.Valuer interface.
func (oids orderIDs) Value() (driver.Value, error) {
	if oids == nil {
		return nil, nil
	}
	if len(oids) == 0 {
		return "{}", nil
	}

	ba := make(pq.ByteaArray, 0, len(oids))
	for i := range oids {
		ba = append(ba, oids[i][:])
	}
	return ba.Value()
}

// Scan implements the sql.Scanner interface.
func (oids *orderIDs) Scan(src any) error {
	var ba pq.ByteaArray
	err := ba.Scan(src)
	if err != nil {
		return err
	}

	n := len(ba)
	*oids = make([]order.OrderID, n)
	for i := range ba {
		copy((*oids)[i][:], ba[i])
	}
	return nil
}

// InsertEpoch stores the results of a newly-processed epoch. TODO: test.
func (a *Archiver) InsertEpoch(ed *db.EpochResults) error {
	marketSchema, err := a.marketSchema(ed.MktBase, ed.MktQuote)
	if err != nil {
		return err
	}

	epochsTableName := fullEpochsTableName(a.dbName, marketSchema)
	stmt := fmt.Sprintf(internal.InsertEpoch, epochsTableName)

	_, err = a.db.Exec(stmt, ed.Idx, ed.Dur, ed.MatchTime, ed.CSum, ed.Seed,
		orderIDs(ed.OrdersRevealed), orderIDs(ed.OrdersMissed))
	if err != nil {
		a.fatalBackendErr(err)
		return err
	}

	epochReportsTableName := fullEpochReportsTableName(a.dbName, marketSchema)
	stmt = fmt.Sprintf(internal.InsertEpochReport, epochReportsTableName)
	epochEnd := (ed.Idx + 1) * ed.Dur
	_, err = a.db.Exec(stmt, epochEnd, ed.Dur, ed.MatchVolume, ed.QuoteVolume, ed.BookBuys, ed.BookBuys5, ed.BookBuys25,
		ed.BookSells, ed.BookSells5, ed.BookSells25, ed.HighRate, ed.LowRate, ed.StartRate, ed.EndRate)
	if err != nil {
		a.fatalBackendErr(err)
	}

	return err
}

// LastEpochRate gets the EndRate of the last EpochResults inserted for the
// market. If the database is empty, no error and a rate of zero are returned.
func (a *Archiver) LastEpochRate(base, quote uint32) (rate uint64, err error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return 0, err
	}

	epochReportsTableName := fullEpochReportsTableName(a.dbName, marketSchema)
	stmt := fmt.Sprintf(internal.SelectLastEpochRate, epochReportsTableName)
	if err = a.db.QueryRowContext(a.ctx, stmt).Scan(&rate); err != nil && !errors.Is(sql.ErrNoRows, err) {
		return 0, err
	}
	return rate, nil
}

// LoadEpochStats reads all market epoch history from the database, updating the
// provided caches along the way.
func (a *Archiver) LoadEpochStats(base, quote uint32, caches []*candles.Cache) error {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return err
	}
	epochReportsTableName := fullEpochReportsTableName(a.dbName, marketSchema)

	ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
	defer cancel()

	// First. load stored candles from the candles table. Establish a start
	// stamp for scanning epoch reports for partial candles.
	var oldestNeeded uint64 = math.MaxUint64
	sinceCaches := make(map[uint64]*candles.Cache, 0) // maps oldest end stamp
	now := uint64(time.Now().UnixMilli())
	for _, cache := range caches {
		if err = a.loadCandles(base, quote, cache, candles.CacheSize); err != nil {
			return fmt.Errorf("loadCandles: %w", err)
		}

		var since uint64
		if len(cache.Candles) > 0 {
			// If we have candles, set our since value to the next expected
			// epoch stamp.
			idx := cache.Last().EndStamp / cache.BinSize
			since = (idx + 1) * cache.BinSize
		} else {
			since = now - (cache.BinSize * candles.CacheSize)
			since = since - since%cache.BinSize // truncate to first end stamp of the epoch
		}
		if since < oldestNeeded {
			oldestNeeded = since
		}
		sinceCaches[since] = cache
	}

	tstart := time.Now()
	defer func() { log.Debugf("select epoch candles in: %v", time.Since(tstart)) }()

	stmt := fmt.Sprintf(internal.SelectEpochCandles, epochReportsTableName)
	rows, err := a.db.QueryContext(ctx, stmt, oldestNeeded) // +1 because candles aren't stored until the end stamp is surpassed.
	if err != nil {
		return fmt.Errorf("SelectEpochCandles: %w", err)
	}

	defer rows.Close()

	var endStamp, epochDur, matchVol, quoteVol, highRate, lowRate, startRate, endRate fastUint64
	for rows.Next() {
		err = rows.Scan(&endStamp, &epochDur, &matchVol, &quoteVol, &highRate, &lowRate, &startRate, &endRate)
		if err != nil {
			return fmt.Errorf("Scan: %w", err)
		}
		candle := &candles.Candle{
			StartStamp:  uint64(endStamp - epochDur),
			EndStamp:    uint64(endStamp),
			MatchVolume: uint64(matchVol),
			QuoteVolume: uint64(quoteVol),
			HighRate:    uint64(highRate),
			LowRate:     uint64(lowRate),
			StartRate:   uint64(startRate),
			EndRate:     uint64(endRate),
		}
		for since, cache := range sinceCaches {
			if uint64(endStamp) > since {
				cache.Add(candle)
			}
		}
	}

	return rows.Err()
}

// LastCandleEndStamp pulls the last stored candles end stamp for a market and
// candle duration.
func (a *Archiver) LastCandleEndStamp(base, quote uint32, candleDur uint64) (uint64, error) {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return 0, err
	}

	tableName := fullCandlesTableName(a.dbName, marketSchema, candleDur)

	ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
	defer cancel()

	stmt := fmt.Sprintf(internal.SelectLastEndStamp, tableName)
	row := a.db.QueryRowContext(ctx, stmt)
	var endStamp fastUint64
	if err = row.Scan(&endStamp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return uint64(endStamp), nil
}

// InsertCandles inserts new candles for a market and candle duration.
func (a *Archiver) InsertCandles(base, quote uint32, candleDur uint64, cs []*candles.Candle) error {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return err
	}
	tableName := fullCandlesTableName(a.dbName, marketSchema, candleDur)
	stmt := fmt.Sprintf(internal.InsertCandle, tableName)

	insert := func(c *candles.Candle) error {
		ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
		defer cancel()

		_, err = a.db.ExecContext(ctx, stmt,
			c.EndStamp, c.MatchVolume, c.QuoteVolume, c.HighRate, c.LowRate, c.StartRate, c.EndRate,
		)
		if err != nil {
			a.fatalBackendErr(err)
			return err
		}
		return nil
	}

	for _, c := range cs {
		if err = insert(c); err != nil {
			return err
		}
	}
	return nil
}

// loadCandles loads the last n candles of a specified duration and market into
// the provided cache.
func (a *Archiver) loadCandles(base, quote uint32, cache *candles.Cache, n uint64) error {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return err
	}

	candleDur := cache.BinSize

	tableName := fullCandlesTableName(a.dbName, marketSchema, candleDur)
	stmt := fmt.Sprintf(internal.SelectCandles, tableName)

	ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
	defer cancel()

	rows, err := a.db.QueryContext(ctx, stmt, n)
	if err != nil {
		return fmt.Errorf("QueryContext: %w", err)
	}
	defer rows.Close()

	var endStamp, matchVol, quoteVol, highRate, lowRate, startRate, endRate fastUint64
	for rows.Next() {
		err = rows.Scan(&endStamp, &matchVol, &quoteVol, &highRate, &lowRate, &startRate, &endRate)
		if err != nil {
			return fmt.Errorf("Scan: %w", err)
		}
		cache.Add(&candles.Candle{
			StartStamp:  uint64(endStamp) - candleDur,
			EndStamp:    uint64(endStamp),
			MatchVolume: uint64(matchVol),
			QuoteVolume: uint64(quoteVol),
			HighRate:    uint64(highRate),
			LowRate:     uint64(lowRate),
			StartRate:   uint64(startRate),
			EndRate:     uint64(endRate),
		})
	}

	if err = rows.Err(); err != nil {
		return err
	}

	return nil
}
