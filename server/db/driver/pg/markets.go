// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"database/sql"
	"fmt"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/db/driver/pg/internal"
)

func loadMarkets(db *sql.DB, marketsTableName string) ([]*dex.MarketInfo, error) {
	stmt := fmt.Sprintf(internal.SelectAllMarkets, marketsTableName)
	rows, err := db.Query(stmt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mkts []*dex.MarketInfo
	for rows.Next() {
		var name string
		var base, quote uint32
		var lot_size uint64
		err = rows.Scan(&name, &base, &quote, &lot_size)
		if err != nil {
			return nil, err
		}
		mkts = append(mkts, &dex.MarketInfo{
			Name:    name,
			Base:    base,
			Quote:   quote,
			LotSize: lot_size,
		})
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return mkts, nil
}

func newMarket(db *sql.DB, marketsTableName string, mkt *dex.MarketInfo) error {
	stmt := fmt.Sprintf(internal.InsertMarket, marketsTableName)
	res, err := db.Exec(stmt, mkt.Name, mkt.Base, mkt.Quote, mkt.LotSize)
	if err != nil {
		return err
	}
	N, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if N != 1 {
		return fmt.Errorf("failed to insert market, %d rows affected", N)
	}
	return nil
}

// Basic idempotent table alteration queries for existing markets tables with a
// printf specifier for full name of the targeted table. Remove this when scheme
// versioning and upgrades are implemented (and v1 has the changes already).
var marketTablesUpdates = map[string]string{
	matchesTableName: internal.AddMatchesForgivenColumn,
}

func createMarketTables(db *sql.DB, marketUID string) error {
	created, err := createSchema(db, marketUID)
	if err != nil {
		return err
	}
	if !created {
		log.Tracef(`Market schema "%s" already exists.`, marketUID)
	}

	for _, c := range createMarketTableStatements {
		newTable, err := CreateTable(db, marketUID, c.name)
		if err != nil {
			return err
		}
		if newTable {
			if !created {
				log.Warnf(`Created missing table "%s" for existing market %s.`,
					c.name, marketUID)
			}
		} else if update, found := marketTablesUpdates[c.name]; found {
			// Remove this with actual upgrades.
			nameSpacedTable := marketUID + "." + c.name
			if _, err = db.Exec(fmt.Sprintf(update, nameSpacedTable)); err != nil {
				return fmt.Errorf("failed to update table %v: %w", nameSpacedTable, err)
			}
		}
	}

	return nil
}
