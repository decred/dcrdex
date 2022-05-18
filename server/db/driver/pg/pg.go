// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package pg

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/db"
)

// Driver implements db.Driver.
type Driver struct{}

// Open creates the DB backend, returning a DEXArchivist.
func (d *Driver) Open(ctx context.Context, cfg interface{}) (db.DEXArchivist, error) {
	switch c := cfg.(type) {
	case *Config:
		return NewArchiver(ctx, c)
	case Config:
		return NewArchiver(ctx, &c)
	default:
		return nil, fmt.Errorf("invalid config type %t", cfg)
	}
}

// UseLogger sets the package-wide logger for the registered DB Driver.
func (*Driver) UseLogger(logger dex.Logger) {
	UseLogger(logger)
}

func init() {
	db.Register("pg", &Driver{})
}

const (
	defaultQueryTimeout = 20 * time.Minute
)

// Config holds the Archiver's configuration.
type Config struct {
	Host, Port, User, Pass, DBName string
	ShowPGConfig                   bool
	QueryTimeout                   time.Duration

	// MarketCfg specifies all of the markets that the Archiver should prepare.
	MarketCfg []*dex.MarketInfo
}

// Some frequently used long-form table names.
type archiverTables struct {
	feeKeys  string
	accounts string
}

// Archiver must implement server/db.DEXArchivist.
// So far: OrderArchiver, AccountArchiver.
type Archiver struct {
	ctx          context.Context
	queryTimeout time.Duration
	db           *sql.DB
	dbName       string
	markets      map[string]*dex.MarketInfo
	tables       archiverTables

	fatalMtx sync.RWMutex
	fatal    chan struct{}
	fatalErr error
}

// LastErr returns any fatal or unexpected error encountered in a recent query.
// This may be used to check if the database had an unrecoverable error
// (disconnect, etc.).
func (a *Archiver) LastErr() error {
	a.fatalMtx.RLock()
	defer a.fatalMtx.RUnlock()
	return a.fatalErr
}

// Fatal returns a nil or closed channel for select use. Use LastErr to get the
// latest fatal error.
func (a *Archiver) Fatal() <-chan struct{} {
	a.fatalMtx.RLock()
	defer a.fatalMtx.RUnlock()
	return a.fatal
}

func (a *Archiver) fatalBackendErr(err error) {
	if err == nil {
		return
	}
	a.fatalMtx.Lock()
	if a.fatalErr == nil {
		close(a.fatal)
	}
	a.fatalErr = err // consider slice and append
	a.fatalMtx.Unlock()
}

// NewArchiverForRead constructs a new Archiver without creating or modifying
// any data structures. This should be used for read-only applications. Use
// Close when done with the Archiver.
func NewArchiverForRead(ctx context.Context, cfg *Config) (*Archiver, error) {
	// Connect to the PostgreSQL daemon and return the *sql.DB.
	db, err := connect(cfg.Host, cfg.Port, cfg.User, cfg.Pass, cfg.DBName)
	if err != nil {
		return nil, err
	}

	// Put the PostgreSQL time zone in UTC.
	var initTZ string
	initTZ, err = checkCurrentTimeZone(db)
	if err != nil {
		return nil, err
	}
	if initTZ != "UTC" {
		log.Infof("Switching PostgreSQL time zone to UTC for this session.")
		if _, err = db.Exec(`SET TIME ZONE UTC`); err != nil {
			return nil, fmt.Errorf("Failed to set time zone to UTC: %w", err)
		}
	}

	// Display the postgres version.
	pgVersion, err := retrievePGVersion(db)
	if err != nil {
		return nil, err
	}
	log.Info(pgVersion)

	queryTimeout := cfg.QueryTimeout
	if queryTimeout <= 0 {
		queryTimeout = defaultQueryTimeout
	}

	mktMap := make(map[string]*dex.MarketInfo, len(cfg.MarketCfg))
	for _, mkt := range cfg.MarketCfg {
		mktMap[marketSchema(mkt.Name)] = mkt
	}

	return &Archiver{
		ctx:          ctx,
		db:           db,
		dbName:       cfg.DBName,
		queryTimeout: queryTimeout,
		markets:      mktMap,
		tables: archiverTables{
			feeKeys:  fullTableName(cfg.DBName, publicSchema, feeKeysTableName),
			accounts: fullTableName(cfg.DBName, publicSchema, accountsTableName),
		},
		fatal: make(chan struct{}),
	}, nil
}

// NewArchiver constructs a new Archiver. All tables are created, including
// tables for markets that may have been added since last startup. Use Close
// when done with the Archiver.
func NewArchiver(ctx context.Context, cfg *Config) (*Archiver, error) {
	archiver, err := NewArchiverForRead(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Check critical performance-related settings.
	if err = archiver.checkPerfSettings(cfg.ShowPGConfig); err != nil {
		return nil, err
	}

	// Ensure all tables required by the current market configuration are ready.
	purgeMarkets, err := prepareTables(ctx, archiver.db, cfg.MarketCfg)
	if err != nil {
		return nil, err
	}
	for _, staleMarket := range purgeMarkets {
		mkt := archiver.markets[staleMarket]
		if mkt == nil { // shouldn't happen
			return nil, fmt.Errorf("unrecognized market %v", staleMarket)
		}
		unbookedSells, unbookedBuys, err := archiver.FlushBook(mkt.Base, mkt.Quote)
		if err != nil {
			return nil, fmt.Errorf("failed to flush book for market %v: %w", staleMarket, err)
		}
		log.Infof("Flushed %d sell orders and %d buy orders from market %v with a changed lot size.",
			len(unbookedSells), len(unbookedBuys), staleMarket)
	}

	return archiver, nil
}

// Close closes the underlying DB connection.
func (a *Archiver) Close() error {
	return a.db.Close()
}

func (a *Archiver) marketSchema(base, quote uint32) (string, error) {
	marketName, err := dex.MarketName(base, quote)
	if err != nil {
		return "", err
	}
	schema := marketSchema(marketName)
	_, found := a.markets[schema]
	if !found {
		return "", db.ArchiveError{
			Code:   db.ErrUnsupportedMarket,
			Detail: fmt.Sprintf(`archiver does not support the market "%s"`, schema),
		}
	}
	return schema, nil
}
