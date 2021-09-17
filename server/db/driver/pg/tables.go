// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"errors"
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
	for _, tbl := range createDEXTableStatements {
		m[tbl.name] = tbl.stmt
	}
	for _, tbl := range createMarketTableStatements {
		m[tbl.name] = tbl.stmt
	}
	for _, tbl := range createAccountTableStatements {
		m[tbl.name] = tbl.stmt
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

// createTable creates one of the known tables by name. The table will be
// created in the specified schema (schema.tableName). If schema is empty,
// "public" is used.
func createTable(db sqlQueryExecutor, schema, tableName string) (bool, error) {
	createCommand, tableNameFound := tableMap[tableName]
	if !tableNameFound {
		return false, fmt.Errorf("table name %q unknown", tableName)
	}

	if schema == "" {
		schema = publicSchema
	}
	return createTableStmt(db, createCommand, schema, tableName)
}

// prepareTables ensures that all tables required by the DEX market config,
// mktConfig, are ready. This also runs any required DB scheme upgrades. The
// Context allows safely canceling upgrades, which may be long running. Returns
// a slice of markets that should have orders flushed due to lot size changes.
func prepareTables(ctx context.Context, db *sql.DB, mktConfig []*dex.MarketInfo) ([]string, error) {
	// Create the markets table in the public schema.
	created, err := createTable(db, publicSchema, marketsTableName)
	if err != nil {
		return nil, fmt.Errorf("failed to create markets table: %w", err)
	}
	if created { // Fresh install
		// Create the meta table in the public schema.
		created, err = createTable(db, publicSchema, metaTableName)
		if err != nil {
			return nil, fmt.Errorf("failed to create meta table: %w", err)
		}
		if !created {
			return nil, fmt.Errorf("existing meta table but no markets table: corrupt DB")
		}
		_, err = db.Exec(internal.CreateMetaRow)
		if err != nil {
			return nil, fmt.Errorf("failed to create row for meta table: %w", err)
		}
		err = setDBVersion(db, dbVersion) // no upgrades
		if err != nil {
			return nil, fmt.Errorf("failed to set db version in meta table: %w", err)
		}
		log.Infof("Created new meta table at version %d", dbVersion)

		// Prepare the account and registration key counter tables.
		err = createAccountTables(db)
		if err != nil {
			return nil, err
		}
	} else {
		// Attempt upgrade.
		if err = upgradeDB(ctx, db); err != nil {
			// If the context is canceled, it will either be context.Canceled
			// from db.BeginTx, or sql.ErrTxDone from any of the tx operations.
			if errors.Is(err, context.Canceled) || errors.Is(err, sql.ErrTxDone) {
				return nil, fmt.Errorf("upgrade DB canceled: %w", err)
			}
			return nil, fmt.Errorf("upgrade DB failed: %w", err)
		}
	}

	// Verify config of existing markets, creating a new markets table if none
	// exists. This is done after upgrades since it can create new tables with
	// the current DB scheme for newly configured markets.
	log.Infof("Configuring %d markets tables: %v", len(mktConfig), mktConfig)
	return prepareMarkets(db, mktConfig)
}

// prepareMarkets ensures that the market-specific tables required by the DEX
// market config, mktConfig, are ready. See also prepareTables.
func prepareMarkets(db *sql.DB, mktConfig []*dex.MarketInfo) ([]string, error) {
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

	var purgeMarkets []string
	// Create any markets in the config that do not already exist. Also create
	// any missing tables for existing markets.
	for _, mkt := range mktConfig {
		existingMkt := marketMap[mkt.Name]
		if existingMkt == nil {
			log.Infof("New market specified in config: %s", mkt.Name)
			err = newMarket(db, marketsTableName, mkt)
			if err != nil {
				return nil, fmt.Errorf("newMarket failed: %w", err)
			}
		} else {
			if mkt.LotSize != existingMkt.LotSize {
				err = updateLotSize(db, publicSchema, mkt.Name, mkt.LotSize)
				if err != nil {
					return nil, fmt.Errorf("unable to update lot size for %s: %w", mkt.Name, err)
				}
				purgeMarkets = append(purgeMarkets, mkt.Name)
			}
		}

		// Create the tables in the markets schema.
		err = createMarketTables(db, mkt.Name)
		if err != nil {
			return nil, fmt.Errorf("createMarketTables failed: %w", err)
		}
	}

	return purgeMarkets, nil
}

// updateLotSize updates the lot size for a market. Must only be called on an
// existing market.
func updateLotSize(db sqlQueryExecutor, schema, mktName string, lotSize uint64) error {
	if schema == "" {
		schema = publicSchema
	}
	nameSpacedTable := schema + "." + marketsTableName
	stmt := fmt.Sprintf(internal.UpdateLotSize, nameSpacedTable)
	_, err := db.Exec(stmt, mktName, lotSize)
	if err != nil {
		return err
	}
	log.Debugf("Updated %s lot size to %d.", mktName, lotSize)
	return nil
}
