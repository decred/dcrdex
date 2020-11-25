// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"database/sql"
	"fmt"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/db/driver/pg/internal"
)

const (
	marketsTableName  = "markets"
	metaTableName     = "meta"
	feeKeysTableName  = "fee_keys"
	accountsTableName = "accounts"

	// market schema tables
	matchesTableName         = "matches"
	epochsTableName          = "epochs"
	ordersArchivedTableName  = "orders_archived"
	ordersActiveTableName    = "orders_active"
	cancelsArchivedTableName = "cancels_archived"
	cancelsActiveTableName   = "cancels_active"
	epochReportsTableName    = "epoch_reports"
)

type tableStmt struct {
	name string
	stmt string
}

var createDEXTableStatements = []tableStmt{
	{marketsTableName, internal.CreateMarketsTable},
	{metaTableName, internal.CreateMetaTable},
}

var createAccountTableStatements = []tableStmt{
	{feeKeysTableName, internal.CreateFeeKeysTable},
	{accountsTableName, internal.CreateAccountsTable},
}

var createMarketTableStatements = []tableStmt{
	{ordersArchivedTableName, internal.CreateOrdersTable},
	{ordersActiveTableName, internal.CreateOrdersTable},
	{cancelsArchivedTableName, internal.CreateCancelOrdersTable},
	{cancelsActiveTableName, internal.CreateCancelOrdersTable},
	{matchesTableName, internal.CreateMatchesTable}, // just one matches table per market for now
	{epochsTableName, internal.CreateEpochsTable},
	{epochReportsTableName, internal.CreateEpochReportTable},
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
		orderTable = ordersActiveTableName
	} else {
		orderTable = ordersArchivedTableName
	}

	return fullTableName(dbName, marketSchema, orderTable)
}

func fullCancelOrderTableName(dbName, marketSchema string, active bool) string {
	var orderTable string
	if active {
		orderTable = cancelsActiveTableName
	} else {
		orderTable = cancelsArchivedTableName
	}

	return fullTableName(dbName, marketSchema, orderTable)
}

func fullMatchesTableName(dbName, marketSchema string) string {
	return dbName + "." + marketSchema + "." + matchesTableName
}

func fullEpochsTableName(dbName, marketSchema string) string {
	return dbName + "." + marketSchema + "." + epochsTableName
}

func fullEpochReportsTableName(dbName, marketSchema string) string {
	return dbName + "." + marketSchema + "." + epochReportsTableName
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
	// Create the meta table in the public schema. (TODO with version).
	// created, err := CreateTable(db, publicSchema, metaTableName)
	// if err != nil {
	// 	return fmt.Errorf("failed to create meta table: %w", err)
	// }
	// if created {
	// 	log.Trace("Creating new meta table.")
	// 	_, err := db.Exec(internal.CreateMetaRow)
	// 	if err != nil {
	// 		return fmt.Errorf("failed to create row for meta table")
	// 	}
	// }

	// TODO: do something about the existing meta table (add version row if not
	// exist and delete the swap state hash column).

	// Create the markets table in the public schema.
	created, err := CreateTable(db, publicSchema, marketsTableName)
	if err != nil {
		return fmt.Errorf("failed to create markets table: %w", err)
	}
	if created {
		log.Trace("Creating new markets table.")
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
		return nil, fmt.Errorf("failed to read markets table: %w", err)
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
			log.Tracef("New market specified in config: %s", mkt.Name)
			err = newMarket(db, marketsTableName, mkt)
			if err != nil {
				return nil, fmt.Errorf("newMarket failed: %w", err)
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
			return nil, fmt.Errorf("createMarketTables failed: %w", err)
		}
	}

	return marketMap, nil
}
