// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ui

import (
	"fmt"
	"os"

	"decred.org/dcrdex/dex"
	"github.com/jrick/logrotate/rotator"
)

var (
	// logRotator is one of the logging outputs. It should be closed on
	// application shutdown.
	logRotator   *rotator.Rotator
	debugLevel   string
	log          dex.Logger
	masterLogger = func([]byte) {}
)

// logWriter implements an io.Writer that outputs to three separate
// destinations, a master logger (app journal), a custom logger (view
// journals), and a rotating log file.
type logWriter struct {
	f func([]byte)
}

// Write writes the data in p to all three destinations.
func (w logWriter) Write(p []byte) (n int, err error) {
	w.f(p)
	masterLogger(p)
	return logRotator.Write(p)
}

// InitLogging initializes the logging rotater to write logs to logFile and
// create roll files in the same directory. All output will also be provided to
// the provided function. It must be called before the package-global log
// rotator variables are used.
func InitLogging(masterLog func([]byte), lvl string, utc bool) *dex.LoggerMaker {
	debugLevel = lvl
	err := os.MkdirAll(logDirectory, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
		os.Exit(1)
	}
	logRotator, err = rotator.New(logFilename, 32*1024, false, maxLogRolls)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create file rotator: %v\n", err)
		os.Exit(1)
	}
	masterLogger = masterLog
	lm, err := CustomLogMaker(nil, utc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create custom logger: %v\n", err)
		os.Exit(1)
	}
	log = lm.Logger("APP")
	return lm
}

// CustomLogger creates a new logger backend that writes to the central rotating
// log file and also the provided function.
func CustomLogMaker(f func(p []byte), utc bool) (*dex.LoggerMaker, error) {
	if f == nil {
		f = func([]byte) {}
	}
	var opts []dex.BackendOption
	if utc {
		opts = append(opts, dex.InUTC())
	}
	return dex.NewLoggerMaker(logWriter{f: f}, debugLevel, opts...)
}

// Close closes the log rotator.
func Close() {
	if logRotator != nil {
		logRotator.Close()
	}
}

// mustLogger panics if there is an error creating the LoggerMaker. mustLogger
// should only be used during start up, and not, for example, when creating a
// new server from the TUI.
func mustLogger(name string, f func(p []byte), utc ...bool) dex.Logger {
	var utcOpt bool
	if len(utc) > 0 {
		utcOpt = utc[0]
	}
	lm, err := CustomLogMaker(f, utcOpt)
	if err != nil {
		panic("error creating logger " + name)
	}
	return lm.Logger(name)
}
