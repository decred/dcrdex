// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
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
	}
	return err
}
