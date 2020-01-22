// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"fmt"
	"strings"

	"github.com/decred/slog"
)

// Many dcrdex types will take a logger as an argument.
type Logger = slog.Logger

// LoggerMaker allows creation of new log subsystems with predefined levels.
type LoggerMaker struct {
	*slog.Backend
	DefaultLevel slog.Level
	Levels       map[string]slog.Level
}

// NewLoggerMaker parses the debug level string into a new *LoggerMaker. The
// debugLevel string can specify a single verbosity for the entire system: "trace",
// "debug", "info", "warn", "error", "critical", "off".
//
// Or the verbosity can be specified for individual subsystems, separating each
// system by commas and assigning each specifically, A command line might look
// like `--degublevel=CORE=debug,SWAP=trace`.
func NewLoggerMaker(be *slog.Backend, debugLevel string) (*LoggerMaker, error) {
	lm := &LoggerMaker{
		Backend:      be,
		Levels:       make(map[string]slog.Level),
		DefaultLevel: slog.LevelDebug,
	}
	// When the specified string doesn't have any delimiters, treat it as
	// the log level for all subsystems.
	if !strings.Contains(debugLevel, ",") && !strings.Contains(debugLevel, "=") {
		// Validate debug log level.
		lvl, ok := slog.LevelFromString(debugLevel)
		if !ok {
			str := "The specified debug level [%v] is invalid"
			return nil, fmt.Errorf(str, debugLevel)
		}
		lm.DefaultLevel = lvl
		return lm, nil
	}

	// Split the specified string into subsystem/level pairs while detecting
	// issues and update the log levels accordingly.
	levelPairs := strings.Split(debugLevel, ",")
	for _, logLevelPair := range levelPairs {
		if !strings.Contains(logLevelPair, "=") {
			str := "The specified debug level contains an invalid " +
				"subsystem/level pair [%v]"
			return nil, fmt.Errorf(str, logLevelPair)
		}

		// Extract the specified subsystem and log level.
		fields := strings.Split(logLevelPair, "=")
		subsysID, logLevel := fields[0], fields[1]

		// Validate log level.
		lvl, ok := slog.LevelFromString(logLevel)
		if !ok {
			str := "The specified debug level [%v] is invalid"
			return nil, fmt.Errorf(str, logLevel)
		}
		lm.Levels[subsysID] = lvl
	}

	return lm, nil
}

// SubLogger creates a Logger with a subsystem name "parent[name]", using any
// known log level for the parent subsystem, defaulting to the DefaultLevel if
// the parent does not have an explicitly set level.
func (lm *LoggerMaker) SubLogger(parent, name string) Logger {
	logger := lm.Backend.Logger(fmt.Sprintf("%s[%s]", parent, name))
	logger.SetLevel(lm.bestLevel(name))
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

// Logger creates a logger with the provided name, using the log level for that name
// if it was set, otherwise the default log level. This differs from NewLogger, which
// does not look in the Level map for the name.
func (lm *LoggerMaker) Logger(name string) Logger {
	logger := lm.Backend.Logger(name)
	logger.SetLevel(lm.bestLevel(name))
	return logger
}

// bestLevel takes a hierarchical list of logger names, least important to most
// important, and returns the best log level found in the Levels map, else the default.
func (lm *LoggerMaker) bestLevel(lvls ...string) slog.Level {
	lvl := lm.DefaultLevel
	for _, l := range lvls {
		lev, found := lm.Levels[l]
		if found {
			lvl = lev
		}
	}
	return lvl
}
