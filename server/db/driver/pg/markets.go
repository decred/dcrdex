// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"database/sql"
	"fmt"
	"strings"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/db/driver/pg/internal"
)

func loadMarkets(db sqlQueryer, marketsTableName string) ([]*dex.MarketInfo, error) {
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
		var lotSize uint64
		err = rows.Scan(&name, &base, &quote, &lotSize)
		if err != nil {
			return nil, err
		}
		mkts = append(mkts, &dex.MarketInfo{
			Name:    name,
			Base:    base,
			Quote:   quote,
			LotSize: lotSize,
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

func createMarketTables(db *sql.DB, marketName string) error {
	marketUID := marketSchema(marketName)
	newMarket, err := createSchema(db, marketUID)
	if err != nil {
		return err
	}
	if newMarket {
		log.Debugf("Created new market schema %q", marketUID)
	} else {
		log.Tracef("Market schema %q already exists.", marketUID)
	}

	for _, c := range createMarketTableStatements {
		newTable, err := createTable(db, marketUID, c.name)
		if err != nil {
			return err
		}
		if newTable && !newMarket {
			log.Warnf(`Created missing table "%s" for existing market %s.`,
				c.name, marketUID)
		}
	}

	return nil
}

// marketSchema replaces the special token symbol character '.' with the allowed
// PostgreSQL character '$'.
func marketSchema(marketName string) string {
	// '$' separator might only work with PostgreSQL.
	return strings.ReplaceAll(marketName, ".", "$")
}
