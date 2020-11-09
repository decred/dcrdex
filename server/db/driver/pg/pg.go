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
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/hdkeychain/v2"
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

	// CheckedStores checks the tables for existing identical entires before
	// attempting to store new data. This will should not be needed if there are
	// no bugs...
	//CheckedStores bool

	// Net is the current network, and can be one of mainnet, testnet, or simnet.
	Net dex.Network

	// FeeKey is base58-encoded extended public key that will be used for
	// generating fee payment addresses.
	FeeKey string
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
	//checkedStores bool
	markets      map[string]*dex.MarketInfo
	feeKeyBranch *hdkeychain.ExtendedKey
	keyHash      []byte // Store the hash to ref the counter table.
	keyParams    *chaincfg.Params
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

// NewArchiver constructs a new Archiver. Use Close when done with the Archiver.
func NewArchiver(ctx context.Context, cfg *Config) (*Archiver, error) {
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
		mktMap[mkt.Name] = mkt
	}

	archiver := &Archiver{
		ctx:          ctx,
		db:           db,
		dbName:       cfg.DBName,
		queryTimeout: queryTimeout,
		markets:      mktMap,
		//checkedStores: cfg.CheckedStores,
		tables: archiverTables{
			feeKeys:  fullTableName(cfg.DBName, publicSchema, feeKeysTableName),
			accounts: fullTableName(cfg.DBName, publicSchema, accountsTableName),
		},
		fatal: make(chan struct{}),
	}

	// Check critical performance-related settings.
	if err = archiver.checkPerfSettings(cfg.ShowPGConfig); err != nil {
		return nil, err
	}

	// Ensure all tables required by the current market configuration are ready.
	if err = PrepareTables(db, cfg.MarketCfg); err != nil {
		return nil, err
	}

	switch cfg.Net {
	case dex.Mainnet:
		archiver.keyParams = chaincfg.MainNetParams()
	case dex.Testnet:
		archiver.keyParams = chaincfg.TestNet3Params()
	case dex.Simnet:
		archiver.keyParams = chaincfg.SimNetParams()
	default:
		return nil, fmt.Errorf("unknown network %d", cfg.Net)
	}

	// Get the master extended public key.
	masterKey, err := hdkeychain.NewKeyFromString(cfg.FeeKey, archiver.keyParams)
	if err != nil {
		return nil, fmt.Errorf("error parsing master pubkey: %w", err)
	}

	// Get the external branch (= child 0) of the extended pubkey.
	archiver.feeKeyBranch, err = masterKey.Child(0)
	if err != nil {
		return nil, fmt.Errorf("error creating external branch: %w", err)
	}

	// Get a unique ID to serve as an ID for this key in the child counter table.
	archiver.keyHash = dcrutil.Hash160([]byte(cfg.FeeKey))
	if err = archiver.CreateKeyEntry(archiver.keyHash); err != nil {
		return nil, err
	}

	return archiver, nil
}

// Close closes the underlying DB connection.
func (a *Archiver) Close() error {
	return a.db.Close()
}

func (a *Archiver) marketSchema(base, quote uint32) (string, error) {
	marketSchema, err := dex.MarketName(base, quote)
	if err != nil {
		return "", err
	}
	_, found := a.markets[marketSchema]
	if !found {
		return "", db.ArchiveError{
			Code:   db.ErrUnsupportedMarket,
			Detail: fmt.Sprintf(`archiver does not support the market "%s"`, marketSchema),
		}
	}
	return marketSchema, nil
}
