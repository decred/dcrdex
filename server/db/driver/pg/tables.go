// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"database/sql"
	"fmt"

	"github.com/decred/dcrdex/dex"
	"github.com/decred/dcrdex/server/db/driver/pg/internal"
)

const (
	marketsTableName  = "markets"
	feeKeysTableName  = "fee_keys"
	accountsTableName = "accounts"
)

type tableStmt struct {
	name string
	stmt string
}

var createDEXTableStatements = []tableStmt{
	{marketsTableName, internal.CreateMarketsTable},
}

var createAccountTableStatements = []tableStmt{
	{feeKeysTableName, internal.CreateFeeKeysTable},
	{accountsTableName, internal.CreateAccountsTable},
}

var createMarketTableStatements = []tableStmt{
	{"orders_archived", internal.CreateOrdersTable},
	{"orders_active", internal.CreateOrdersTable},
	{"cancels_archived", internal.CreateCancelOrdersTable},
	{"cancels_active", internal.CreateCancelOrdersTable},
	{"matches", internal.CreateMatchesTable}, // just one matches table per market for now
}

var tableMap = func() map[string]string {
	m := make(map[string]string, len(createDEXTableStatements)+
		len(createMarketTableStatements)+len(createAccountTableStatements))
	for _, pair := range createDEXTableStatements {
		m[pair.name] = pair.stmt
	}
	for _, pair := range createMarketTableStatements {
		m[pair.name] = pair.stmt
	}
	for _, pair := range createAccountTableStatements {
		m[pair.name] = pair.stmt
	}
	return m
}()

func fullOrderTableName(dbName, marketSchema string, active bool) string {
	var orderTable string
	if active {
		orderTable = "orders_active"
	} else {
		orderTable = "orders_archived"
	}

	return fullTableName(dbName, marketSchema, orderTable)
}

func fullCancelOrderTableName(dbName, marketSchema string, active bool) string {
	var orderTable string
	if active {
		orderTable = "cancels_active"
	} else {
		orderTable = "cancels_archived"
	}

	return fullTableName(dbName, marketSchema, orderTable)
}

func fullMatchesTableName(dbName, marketSchema string) string {
	return dbName + "." + marketSchema + ".matches"
}

// CreateTable creates one of the known tables by name. The table will be
// created in the specified schema (schema.tableName). If schema is empty,
// "public" is used.
func CreateTable(db *sql.DB, schema, tableName string) (bool, error) {
	createCommand, tableNameFound := tableMap[tableName]
	if !tableNameFound {
		return false, fmt.Errorf("table name %s unknown", tableName)
	}

	if schema == "" {
		schema = publicSchema
	}
	return createTable(db, createCommand, schema, tableName)
}

// PrepareTables ensures that all tables required by the DEX market config,
// mktConfig, are ready.
func PrepareTables(db *sql.DB, mktConfig []*dex.MarketInfo) error {
	// Create the markets table in the public schema.
	created, err := CreateTable(db, publicSchema, marketsTableName)
	if err != nil {
		return fmt.Errorf("failed to create markets table: %v", err)
	}
	if created {
		log.Warn("Creating new markets table.")
	}

	// Verify config of existing markets, creating a new markets table if none
	// exists.
	_, err = prepareMarkets(db, mktConfig)
	if err != nil {
		return err
	}

	// Prepare the account and registration key counter tables.
	err = createAccountTables(db)
	if err != nil {
		return err
	}
	return nil
}

// prepareMarkets ensures that the market-specific tables required by the DEX
// market config, mktConfig, are ready. See also PrepareTables.
func prepareMarkets(db *sql.DB, mktConfig []*dex.MarketInfo) (map[string]*dex.MarketInfo, error) {
	// Load existing markets and ensure there aren't multiple with the same ID.
	mkts, err := loadMarkets(db, marketsTableName)
	if err != nil {
		return nil, fmt.Errorf("failed to read markets table: %v", err)
	}
	marketMap := make(map[string]*dex.MarketInfo, len(mkts))
	for _, mkt := range mkts {
		if _, found := marketMap[mkt.Name]; found {
			// should never happen since market name is (unique) primary key
			panic(fmt.Sprintf(`multiple markets with the same name "%s" found!`,
				mkt.Name))
		}
		marketMap[mkt.Name] = mkt
	}

	// Create any markets in the config that do not already exist. Also create
	// any missing tables for existing markets.
	for _, mkt := range mktConfig {
		existingMkt := marketMap[mkt.Name]
		if existingMkt == nil {
			log.Infof("New market specified in config: %s", mkt.Name)
			err = newMarket(db, marketsTableName, mkt)
			if err != nil {
				return nil, fmt.Errorf("newMarket failed: %v", err)
			}
		} else {
			// TODO: check params, inc. lot size
			if mkt.LotSize != existingMkt.LotSize {
				panic("lot size change: unimplemented") // TODO
			}
		}

		// Create the tables in the markets schema.
		err = createMarketTables(db, mkt.Name)
		if err != nil {
			return nil, fmt.Errorf("createMarketTables failed: %v", err)
		}
	}

	return marketMap, nil
}
