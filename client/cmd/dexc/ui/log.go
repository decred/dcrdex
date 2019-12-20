// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package ui

import (
	"fmt"
	"os"

	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"
)

var (
	// logRotator is one of the logging outputs. It should be closed on
	// application shutdown.
	logRotator   *rotator.Rotator
	log          slog.Logger
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
// the provided function.It must be called before the package-global log rotater
// variables are used.
func InitLogging(masterLog func([]byte)) {
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
	log = NewLogger("APP", nil)
}

// NewLogger creates a new logger that writes to the central rotating log file
// and also the provided function.
func NewLogger(tag string, f func(p []byte)) slog.Logger {
	if f == nil {
		f = func([]byte) {}
	}
	backendLog := slog.NewBackend(logWriter{f: f})
	return backendLog.Logger(tag)
}

// Close closes the log rotator.
func Close() {
	if logRotator != nil {
		logRotator.Close()
	}
}
