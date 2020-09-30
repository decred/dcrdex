// +build pgonline

package pg

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"decred.org/dcrdex/dex"
)

const (
	PGTestsHost   = "localhost" // "/run/postgresql" for UNIX socket
	PGTestsPort   = "5432"      // "" for UNIX socket
	PGTestsUser   = "dcrdex"    // "dcrdex" for full database rather than test data repo
	PGTestsPass   = ""
	PGTestsDBName = "dcrdex_mainnet_test"
)

var (
	archie            *Archiver
	mktInfo, mktInfo2 *dex.MarketInfo
	numMarkets        int
)

func TestMain(m *testing.M) {
	startLogger()

	// Wrap openDB so that the cleanUp function may be deferred.
	doIt := func() int {
		// Not counted as coverage, must test Archiver constructor explicitly.
		cleanUp, err := openDB()
		defer cleanUp()
		if err != nil {
			panic(fmt.Sprintln("no db for testing:", err))
		}

		return m.Run()
	}

	os.Exit(doIt())
}

func openDB() (func() error, error) {
	var err error
	mktInfo, err = dex.NewMarketInfoFromSymbols("dcr", "btc", LotSize, EpochDuration, MarketBuyBuffer)
	if err != nil {
		return func() error { return nil }, fmt.Errorf("invalid market: %v", err)
	}

	mktInfo2, err = dex.NewMarketInfoFromSymbols("btc", "ltc", LotSize, EpochDuration, MarketBuyBuffer)
	if err != nil {
		return func() error { return nil }, fmt.Errorf("invalid market: %v", err)
	}

	AssetDCR = mktInfo.Base
	AssetBTC = mktInfo.Quote
	AssetLTC = mktInfo2.Quote

	dbi := Config{
		Host:         PGTestsHost,
		Port:         PGTestsPort,
		User:         PGTestsUser,
		Pass:         PGTestsPass,
		DBName:       PGTestsDBName,
		ShowPGConfig: false,
		QueryTimeout: 0, // zero to use the default
		MarketCfg:    []*dex.MarketInfo{mktInfo, mktInfo2},
		//CheckedStores: true,
		Net:    dex.Mainnet,
		FeeKey: "dprv3hCznBesA6jBu1MaSqEBewG76yGtnG6LWMtEXHQvh3MVo6rqesTk7FPMSrczDtEELReV4aGMcrDxc9htac5mBDUEbTi9rgCA8Ss5FkasKM3",
	}

	numMarkets = len(dbi.MarketCfg)

	closeFn := func() error { return nil }

	// Low-level db connection to kill any leftover tables.
	db, err := connect(dbi.Host, dbi.Port, dbi.User, dbi.Pass, dbi.DBName)
	if err != nil {
		return closeFn, err
	}
	if err := nukeAll(db); err != nil {
		return closeFn, fmt.Errorf("nukeAll: %v", err)
	}
	if err = db.Close(); err != nil {
		return closeFn, fmt.Errorf("failed to close DB: %v", err)
	}

	ctx := context.Background()
	archie, err = NewArchiver(ctx, &dbi)
	if archie == nil {
		return closeFn, err
	}

	closeFn = func() error {
		if err := nukeAll(archie.db); err != nil {
			log.Errorf("nukeAll: %v", err)
		}
		return archie.Close()
	}

	return closeFn, err
}

func detectMarkets(db *sql.DB) ([]string, error) {
	// Identify all markets by matching schemas like dcr_btc with '___\____'.
	rows, err := db.Query(`select nspname from pg_catalog.pg_namespace where nspname like '___\____';`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var markets []string
	for rows.Next() {
		var market string
		if err = rows.Scan(&market); err != nil {
			rows.Close()
			return nil, err
		}
		markets = append(markets, market)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return markets, nil
}

// nukeAll removes all of the market schemas and the tables within them, as well
// as all of the DEX tables in the public schema.
// TODO: find a long term home for this once it is clear if and how it will be
// used outside of tests.
func nukeAll(db *sql.DB) error {
	markets, err := detectMarkets(db)
	if err != nil {
		return fmt.Errorf("failed to detect markets: %v", err)
	}

	// Drop market schemas.
	for i := range markets {
		log.Tracef(`Dropping market %s schema...`, markets[i])
		_, err = db.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE;", markets[i]))
		if err != nil {
			return err
		}
	}

	// Drop tables in public schema.
	dropPublic := func(stmts []tableStmt) error {
		for i := range stmts {
			tableName := "public." + stmts[i].name
			log.Tracef(`Dropping DEX table %s...`, tableName)
			if err = dropTable(db, tableName); err != nil {
				return err
			}
		}
		return nil
	}

	err = dropPublic(createDEXTableStatements)
	if err != nil {
		return err
	}

	return dropPublic(createAccountTableStatements)
}

func cleanTables(db *sql.DB) error {
	err := nukeAll(db)
	if err != nil {
		return err
	}
	err = PrepareTables(db, mktConfig())
	if err != nil {
		return err
	}

	return archie.CreateKeyEntry(archie.keyHash)
}

func Test_sqlExec(t *testing.T) {
	// Expect to update 0 rows.
	stmt := fmt.Sprintf(`UPDATE %s SET lot_size=1234 WHERE name='definitely not a market';`, marketsTableName)
	N, err := sqlExec(archie.db, stmt)
	if err != nil {
		t.Fatal("sqlExec:", err)
	}
	if N != 0 {
		t.Errorf("Should have updated 0 rows without error, got %d", N)
	}

	// Expect to update 1 rows.
	stmt = fmt.Sprintf(`UPDATE %s SET lot_size=lot_size WHERE name=$1;`,
		marketsTableName)
	N, err = sqlExec(archie.db, stmt, mktInfo.Name)
	if err != nil {
		t.Fatal("sqlExec:", err)
	}
	if N != 1 {
		t.Errorf("Should have updated 1 rows without error, got %d", N)
	}

	// Expect to update N=numMarkets rows.
	stmt = fmt.Sprintf(`UPDATE %s SET lot_size=lot_size;`,
		marketsTableName)
	N, err = sqlExec(archie.db, stmt)
	if err != nil {
		t.Fatal("sqlExec:", err)
	}
	if N != int64(numMarkets) {
		t.Errorf("Should have updated %d rows without error, got %d", numMarkets, N)
	}
}

func Test_checkCurrentTimeZone(t *testing.T) {
	currentTZ, err := checkCurrentTimeZone(archie.db)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Set time zone: %v", currentTZ)
}

func Test_retrieveSysSettingsConfFile(t *testing.T) {
	ss, err := retrieveSysSettingsConfFile(archie.db)
	if err != nil && err != sql.ErrNoRows {
		t.Errorf("Failed to retrieve system settings: %v", err)
	}
	t.Logf("\n%v", ss)
}

func Test_retrieveSysSettingsPerformance(t *testing.T) {
	ss, err := retrieveSysSettingsPerformance(archie.db)
	if err != nil {
		t.Errorf("Failed to retrieve system settings: %v", err)
	}
	t.Logf("\n%v", ss)
}

func Test_retrieveSysSettingsServer(t *testing.T) {
	ss, err := retrieveSysSettingsServer(archie.db)
	if err != nil {
		t.Errorf("Failed to retrieve system server: %v", err)
	}
	t.Logf("\n%v", ss)
}

func Test_retrievePGVersion(t *testing.T) {
	ver, err := retrievePGVersion(archie.db)
	if err != nil {
		t.Errorf("Failed to retrieve postgres version: %v", err)
	}
	t.Logf("\n%s", ver)
}
