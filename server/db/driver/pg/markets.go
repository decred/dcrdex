// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"database/sql"
	"fmt"

	"github.com/decred/dcrdex/server/db/driver/pg/internal"
	"github.com/decred/dcrdex/server/market/types"
)

func loadMarkets(db *sql.DB, marketsTableName string) ([]*types.MarketInfo, error) {
	stmt := fmt.Sprintf(internal.SelectAllMarkets, marketsTableName)
	rows, err := db.Query(stmt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var mkts []*types.MarketInfo
	for rows.Next() {
		var name string
		var base, quote uint32
		var lot_size uint64
		err = rows.Scan(&name, &base, &quote, &lot_size)
		if err != nil {
			return nil, err
		}
		mkts = append(mkts, &types.MarketInfo{
			Name:    name,
			Base:    base,
			Quote:   quote,
			LotSize: lot_size,
		})
	}

	return mkts, nil
}

func createMarketSchema(db *sql.DB, marketUID string) (bool, error) {
	exists, err := schemaExists(db, marketUID)
	if err != nil {
		return false, err
	}

	var created bool
	if !exists {
		log.Infof(`Creating the market namespace "%s".`, marketUID)
		stmt := fmt.Sprintf(internal.CreateMarketSchema, marketUID)
		_, err = db.Exec(stmt)
		if err != nil {
			return false, err
		}
		created = true
	} else {
		log.Tracef(`Schema "%s" exists.`, marketUID)
	}

	return created, err
}

func createMarketTables(db *sql.DB, marketUID string) error {
	newMtk, err := createMarketSchema(db, marketUID)
	if err != nil {
		return err
	}
	if !newMtk {
		log.Debugf(`Market schema "%s" already exists.`, marketUID)
	}

	for _, c := range createMarketTableStatements {
		newTable, err := CreateTable(db, marketUID, c.name)
		if err != nil {
			return err
		}
		if newTable && !newMtk {
			log.Warnf(`Created missing table "%s" for existing market %s.`,
				c.name, marketUID)
		}
	}

	return nil
}
