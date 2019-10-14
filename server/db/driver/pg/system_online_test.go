// +build pgonline

package pg

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/decred/dcrdex/server/market/types"
)

const (
	PGTestsHost   = "localhost" // "/run/postgresql" for UNIX socket
	PGTestsPort   = "5432"      // "" for UNIX socket
	PGTestsUser   = "dcrdex"    // "dcrdex" for full database rather than test data repo
	PGTestsPass   = ""
	PGTestsDBName = "dcrdex_mainnet_test"
)

var (
	archie  *Archiver
	sqlDb   *sql.DB
	mktInfo *types.MarketInfo
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
	mktInfo, err = types.NewMarketInfoFromSymbols("dcr", "btc", LotSize)
	if err != nil {
		return func() error { return nil }, fmt.Errorf("invalid market: %v", err)
	}

	AssetDCR = mktInfo.Base
	AssetBTC = mktInfo.Quote

	dbi := Config{
		Host:          PGTestsHost,
		Port:          PGTestsPort,
		User:          PGTestsUser,
		Pass:          PGTestsPass,
		DBName:        PGTestsDBName,
		HidePGConfig:  true,
		QueryTimeout:  0, // zero to use the default
		MarketCfg:     []*types.MarketInfo{mktInfo},
		CheckedStores: true,
	}
	ctx := context.Background()
	archie, err = NewArchiver(ctx, &dbi)
	if archie == nil {
		return func() error { return nil }, err
	}

	closeFn := func() error {
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

	var markets []string
	for rows.Next() {
		var market string
		if err = rows.Scan(&market); err != nil {
			rows.Close()
			return nil, err
		}
		markets = append(markets, market)
	}
	rows.Close()

	return markets, nil
}

// nukeAll removes all of the market schemas and the tables within them, as well
// as all of the DEX tables in the public schema.
// TODO: find a long term home for this once it is clear if and how it will be
// used outside fo tests.
func nukeAll(db *sql.DB) error {
	markets, err := detectMarkets(db)
	if err != nil {
		return fmt.Errorf("failed to detect markets: %v", err)
	}

	// Drop market schemas.
	for i := range markets {
		log.Infof(`Dropping market %s schema...`, markets[i])
		_, err = db.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE;", markets[i]))
		if err != nil {
			return err
		}
	}

	// Drop tables in public schema.
	for i := range createPublicTableStatements {
		tableName := "public." + createPublicTableStatements[i].name
		log.Infof(`Dropping DEX table %s...`, tableName)
		if err = dropTable(db, tableName); err != nil {
			return err
		}
	}

	return nil
}

func cleanTables(db *sql.DB) error {
	err := nukeAll(db)
	if err != nil {
		return err
	}

	return PrepareTables(db, mktConfig())
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
