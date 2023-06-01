// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package app

import (
	"fmt"
	"os"
	"path/filepath"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"github.com/decred/slog"
	"github.com/jrick/logrotate/rotator"
)

const (
	maxLogRolls = 16
)

var (
	// logRotator is one of the logging outputs. It should be closed on
	// application shutdown.
	logRotator         *rotator.Rotator
	defaultLogLevelMap = map[string]slog.Level{asset.InternalNodeLoggerName: slog.LevelError}
)

// logWriter implements an io.Writer that outputs to a rotating log file.
type logWriter struct {
	*rotator.Rotator
	stdout bool
}

// Write writes the data in p to the log file.
func (w logWriter) Write(p []byte) (n int, err error) {
	if w.stdout {
		os.Stdout.Write(p)
	}
	return w.Rotator.Write(p)
}

// initLogging initializes the logging rotater to write logs to logFile and
// create roll files in the same directory. initLogging must be called before
// the package-global log rotator variables are used.
func InitLogging(logFilename, lvl string, stdout bool, utc bool) (lm *dex.LoggerMaker, closeFn func()) {
	logDirectory := filepath.Dir(logFilename)
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
	if !stdout {
		fmt.Println("Logging to", logFilename)
	}
	lm, err = dex.NewLoggerMaker(&logWriter{logRotator, stdout}, lvl, utc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create custom logger: %v\n", err)
		os.Exit(1)
	}
	lm.SetLevelsFromMap(defaultLogLevelMap)
	return lm, func() {
		logRotator.Close()
	}
}
