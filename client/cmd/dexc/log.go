// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"fmt"
	"os"

	"decred.org/dcrdex/dex"
	"github.com/jrick/logrotate/rotator"
)

var (
	// logRotator is one of the logging outputs. It should be closed on
	// application shutdown.
	logRotator *rotator.Rotator
	log        dex.Logger
)

// logWriter implements an io.Writer that outputs to three separate
// destinations, a master logger (app journal), a custom logger (view
// journals), and a rotating log file.
type logWriter struct{}

// Write writes the data in p to all three destinations.
func (w logWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	return logRotator.Write(p)
}

// InitLogging initializes the logging rotater to write logs to logFile and
// create roll files in the same directory. All output will also be provided to
// the provided function. It must be called before the package-global log
// rotator variables are used.
func initLogging(debugLevel string, utc bool) *dex.LoggerMaker {
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
	lm, err := dex.NewLoggerMaker(logWriter{}, debugLevel, utc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create custom logger: %v\n", err)
		os.Exit(1)
	}
	log = lm.Logger("APP")
	return lm
}

// closeFileLogger closes the log rotator.
func closeFileLogger() {
	if logRotator != nil {
		logRotator.Close()
	}
}
