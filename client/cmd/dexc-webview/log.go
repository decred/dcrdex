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

// initLogging initializes the logging rotater to write logs to logFile and
// create roll files in the same directory. initLogging must be called before
// the package-global log rotator variables are used.
func initLogging(lvl string, utc bool) *dex.LoggerMaker {
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
	fmt.Println("dexc-webview is logging to ", logFilename)
	lm, err := dex.NewLoggerMaker(logRotator, lvl, utc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create custom logger: %v\n", err)
		os.Exit(1)
	}
	lm.SetLevelsFromMap(defaultLogLevelMap)
	log = lm.Logger("APP")
	return lm
}

// closeFileLogger closes the log rotator.
func closeFileLogger() {
	if logRotator != nil {
		logRotator.Close()
	}
}
