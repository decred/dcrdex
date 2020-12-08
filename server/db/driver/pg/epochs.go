// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql/driver"
	"fmt"

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
func (oids *orderIDs) Scan(src interface{}) error {
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

// LoadEpochStats reads all market epoch history from the database, updating the
// provided caches along the way.
func (a *Archiver) LoadEpochStats(base, quote uint32, caches []*db.CandleCache) error {
	marketSchema, err := a.marketSchema(base, quote)
	if err != nil {
		return err
	}
	epochReportsTableName := fullEpochReportsTableName(a.dbName, marketSchema)

	ctx, cancel := context.WithTimeout(a.ctx, a.queryTimeout)
	defer cancel()

	stmt := fmt.Sprintf(internal.SelectEpochCandles, epochReportsTableName)
	rows, err := a.db.QueryContext(ctx, stmt, 0)
	if err != nil {
		return err
	}

	defer rows.Close()

	for rows.Next() {
		candle := new(db.Candle)
		var epochDur uint64
		err = rows.Scan(&candle.EndStamp, &epochDur, &candle.MatchVolume, &candle.QuoteVolume, &candle.HighRate, &candle.LowRate, &candle.StartRate, &candle.EndRate)
		if err != nil {
			return err
		}
		candle.StartStamp = candle.EndStamp - epochDur
		for _, set := range caches {
			set.Add(candle)
		}
	}

	return err
}
