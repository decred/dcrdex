// +build pgonline

package pg

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/decred/dcrdex/server/db"
	"github.com/decred/slog"
)

const (
	PGTestsHost   = "localhost" // "/run/postgresql" for UNIX socket
	PGTestsPort   = "5432"      // "" for UNIX socket
	PGTestsUser   = "dcrdex"    // "dcrdex" for full database rather than test data repo
	PGTestsPass   = ""
	PGTestsDBName = "dcrdex_mainnet_test"
)

var (
	archie *Archiver
	sqlDb  *sql.DB
)

func openDB() (func() error, error) {
	mktInfo, err := db.NewMarketInfoFromSymbols("dcr", "btc", LotSize)
	if err != nil {
		return func() error { return nil }, fmt.Errorf("invalid market: %v", err)
	}

	AssetDCR = mktInfo.Base
	AssetBTC = mktInfo.Quote

	dbi := Config{
		Host:         PGTestsHost,
		Port:         PGTestsPort,
		User:         PGTestsUser,
		Pass:         PGTestsPass,
		DBName:       PGTestsDBName,
		HidePGConfig: false,
		QueryTimeout: 5 * time.Minute,
		MarketCfg:    []*db.MarketInfo{mktInfo},
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

func TestMain(m *testing.M) {
	// Setup pg logger.
	UseLogger(slog.NewBackend(os.Stdout).Logger("pg"))
	log.SetLevel(slog.LevelTrace)

	// Wrap openDB so that the cleanUp function may be deferred.
	doIt := func() int {
		cleanUp, err := openDB()
		defer cleanUp()
		if err != nil {
			panic(fmt.Sprintln("no db for testing:", err))
		}

		return m.Run()
	}

	os.Exit(doIt())
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
