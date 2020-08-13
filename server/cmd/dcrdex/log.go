// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"decred.org/dcrdex/dex"
	"github.com/jrick/logrotate/rotator"
)

// logWriter implements an io.Writer that outputs to both standard output and
// the write-end pipe of an initialized log rotator.
type logWriter struct{}

// Write writes the data in p to standard out and the log rotator.
func (logWriter) Write(p []byte) (n int, err error) {
	if logRotator == nil {
		return os.Stdout.Write(p)
	}
	os.Stdout.Write(p)
	return logRotator.Write(p) // not safe concurrent writes, so only one logWriter{} allowed!
}

// Loggers per subsystem. A single backend logger is created and all subsystem
// loggers created from it will write to the backend. When adding new
// subsystems, define it in the subsystemLoggers map.
//
// For packages with package-level loggers, subsystem logging calls should not
// be done before actually setting the logger in parseAndSetDebugLevels.
//
// Loggers should not be used before the log rotator has been initialized with a
// log file. This must be performed early during application startup by calling
// initLogRotator.
var (
	// logRotator is one of the logging outputs. Use initLogRotator to set it.
	// It should be closed on application shutdown.
	logRotator *rotator.Rotator

	// package main's Logger.
	log = dex.Disabled

	// subsystemLoggers maps each subsystem identifier to its associated logger.
	// The loggers are disabled until parseAndSetDebugLevels is called.
	subsystemLoggers = map[string]dex.Logger{
		"MAIN": dex.Disabled,
		"DEX":  dex.Disabled,
		"DB":   dex.Disabled,
		"COMM": dex.Disabled,
		"AUTH": dex.Disabled,
		"SWAP": dex.Disabled,
		"MKT":  dex.Disabled,
		"BOOK": dex.Disabled,
		"MTCH": dex.Disabled,
		"WAIT": dex.Disabled,
		"ADMN": dex.Disabled,

		// Individual assets get their own subsystem loggers. This is here to
		// register the ASSET subsystem ID, allowing the user to set the log
		// level for the asset subsystems.
		"ASSET": dex.Disabled,
	}
)

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
