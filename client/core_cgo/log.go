//go:build cgo

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"decred.org/dcrdex/dex"
	"github.com/jrick/logrotate/rotator"
)

var (
	// logRotator is one of the logging outputs. It should be closed on
	// application shutdown.
	logRotator *rotator.Rotator
	log        dex.Logger
)

const (
	maxLogRolls = 16
)

// logWriter implements an io.Writer that outputs to stdout
// and a rotating log file.
type logWriter struct{}

// Write writes the data in p to both destinations.
func (w logWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	return logRotator.Write(p)
}

// initLogging initializes the logging rotater to write logs to logFile and
// create roll files in the same directory. initLogging must be called before
// the package-global log rotator variables are used.
func initLogging(logDirectory, lvl string, utc bool) (*dex.LoggerMaker, error) {
	err := os.MkdirAll(logDirectory, 0700)
	if err != nil {
		return nil, fmt.Errorf("failed to create log directory: %v", err)
	}
	logFilename := filepath.Join(logDirectory, "dexc.log")
	logRotator, err = rotator.New(logFilename, 32*1024, false, maxLogRolls)
	if err != nil {
		return nil, fmt.Errorf("failed to create file rotator: %v", err)
	}
	lm, err := dex.NewLoggerMaker(logWriter{}, lvl, utc)
	if err != nil {
		return nil, fmt.Errorf("failed to create custom logger: %v", err)
	}
	log = lm.Logger("APP")
	return lm, nil
}

// closeFileLogger closes the log rotator.
func closeFileLogger() {
	if logRotator != nil {
		logRotator.Close()
	}
}
