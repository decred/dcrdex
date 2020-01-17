// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"fmt"

	"github.com/decred/slog"
)

// Every backend constructor will accept a Logger. All logging should take place
// through the provided logger.
type Logger = slog.Logger

// LoggerMaker allows creation of new log subsystems with predefined levels.
type LoggerMaker struct {
	*slog.Backend
	DefaultLevel slog.Level
	Levels       map[string]slog.Level
}

// SubLogger creates a Logger with a subsystem name "parent[name]", using any
// known log level for the parent subsystem, defaulting to the DefaultLevel if
// the parent does not have an explicitly set level.
func (lm *LoggerMaker) SubLogger(parent, name string) Logger {
	// Use the parent logger's log level, if set.
	level, ok := lm.Levels[parent]
	if !ok {
		level = lm.DefaultLevel
	}
	logger := lm.Backend.Logger(fmt.Sprintf("%s[%s]", parent, name))
	logger.SetLevel(level)
	return logger
}

// NewLogger creates a new Logger for the subsystem with the given name. If a
// log level is specified, it is used for the Logger. Otherwise the DefaultLevel
// is used.
func (lm *LoggerMaker) NewLogger(name string, level ...slog.Level) Logger {
	lvl := lm.DefaultLevel
	if len(level) > 0 {
		lvl = level[0]
	}
	logger := lm.Backend.Logger(name)
	logger.SetLevel(lvl)
	return logger
}
