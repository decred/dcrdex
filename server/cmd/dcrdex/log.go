// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"decred.org/dcrdex/dex/wait"
	"decred.org/dcrdex/dex/ws"
	"decred.org/dcrdex/server/admin"
	"decred.org/dcrdex/server/auth"
	"decred.org/dcrdex/server/book"
	"decred.org/dcrdex/server/comms"
	"decred.org/dcrdex/server/db"
	dexsrv "decred.org/dcrdex/server/dex"
	"decred.org/dcrdex/server/market"
	"decred.org/dcrdex/server/matcher"
	"decred.org/dcrdex/server/swap"
	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"
)

// logWriter implements an io.Writer that outputs to both standard output and
// the write-end pipe of an initialized log rotator.
type logWriter struct{}

// Write writes the data in p to standard out and the log rotator.
func (logWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	return logRotator.Write(p)
}

// Loggers per subsystem. A single backend logger is created and all subsystem
// loggers created from it will write to the backend. When adding new
// subsystems, add the subsystem logger variable here and to the
// subsystemLoggers map.
//
// Loggers can not be used before the log rotator has been initialized with a
// log file. This must be performed early during application startup by calling
// initLogRotator.
var (
	// backendLog is the logging backend used to create all subsystem loggers.
	// The backend must not be used before the log rotator has been initialized,
	// or data races and/or nil pointer dereferences will occur.
	backendLog = slog.NewBackend(logWriter{})

	// logRotator is one of the logging outputs. Use initLogRotator to set it.
	// It should be closed on application shutdown.
	logRotator *rotator.Rotator

	log           = backendLog.Logger("MAIN")
	dbLogger      = backendLog.Logger("DB")
	dexmanLogger  = backendLog.Logger("DEX")
	commsLogger   = backendLog.Logger("COMM")
	authLogger    = backendLog.Logger("AUTH")
	swapLogger    = backendLog.Logger("SWAP")
	marketLogger  = backendLog.Logger("MKT")
	bookLogger    = backendLog.Logger("BOOK")
	matcherLogger = backendLog.Logger("MTCH")
	waiterLogger  = backendLog.Logger("CHWT")
	adminLogger   = backendLog.Logger("ADMN")
)

func init() {
	auth.UseLogger(authLogger)
	comms.UseLogger(commsLogger)
	ws.UseLogger(commsLogger)
	db.UseLogger(dbLogger)
	dexsrv.UseLogger(dexmanLogger)
	market.UseLogger(marketLogger)
	swap.UseLogger(swapLogger)
	book.UseLogger(bookLogger)
	matcher.UseLogger(matcherLogger)
	wait.UseLogger(waiterLogger)
	admin.UseLogger(adminLogger)
}

// subsystemLoggers maps each subsystem identifier to its associated logger.
var subsystemLoggers = map[string]slog.Logger{
	"MAIN": log,
	"DB":   dbLogger,
	"DEX":  dexmanLogger,
	"COMM": commsLogger,
	"AUTH": authLogger,
	"SWAP": swapLogger,
	"MKT":  marketLogger,
	// Individual assets get their own subsystem loggers. This is here to
	// register the ASSET subsystem ID, allowing the user to set the log level
	// for the asset subsystems.
	"ASSET": slog.Disabled,
	"BOOK":  bookLogger,
	"MTCH":  matcherLogger,
	"ADMN":  adminLogger,
}

// initLogRotator initializes the logging rotater to write logs to logFile and
// create roll files in the same directory.  It must be called before the
// package-global log rotater variables are used.
func initLogRotator(logFile string, maxRolls int) {
	logDir, _ := filepath.Split(logFile)
	err := os.MkdirAll(logDir, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
		os.Exit(1)
	}
	logRotator, err = rotator.New(logFile, 32*1024, false, maxRolls)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create file rotator: %v\n", err)
		os.Exit(1)
	}
}

// setLogLevel sets the logging level for provided subsystem. Invalid subsystems
// are ignored.
func setLogLevel(subsystemID string, level slog.Level) {
	// Ignore invalid subsystems.
	logger, ok := subsystemLoggers[subsystemID]
	if !ok {
		return
	}
	logger.SetLevel(level)
}

// setLogLevels sets the log level for all subsystem loggers to the passed
// level.
func setLogLevels(level slog.Level) {
	// Configure all sub-systems with the new logging level.
	for subsystemID := range subsystemLoggers {
		setLogLevel(subsystemID, level)
	}
}
