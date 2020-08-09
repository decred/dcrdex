// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/decred/slog"
)

func init() {
	Disabled = &log{
		Logger:  slog.Disabled,
		level:   slog.LevelOff,
		backend: slog.NewBackend(ioutil.Discard),
	}
}

// Logger is a logger. Many dcrdex types will take a logger as an argument.
type Logger interface {
	slog.Logger
	SubLogger(name string) Logger
}

// Disabled is a Logger that will never output anything.
var Disabled Logger

// LoggerMaker allows creation of new log subsystems with predefined levels.
type LoggerMaker struct {
	*slog.Backend
	DefaultLevel slog.Level
	Levels       map[string]slog.Level
}

// log contains the slog.Logger and fields needed to spawn subloggers. It
// satisfies the Logger interface.
type log struct {
	slog.Logger
	name    string
	level   slog.Level
	levels  map[string]slog.Level // may be nil
	backend *slog.Backend
}

// SubLogger creates a new Logger for the subsystem with the given name. If name
// exists in the levels map, use that level, otherwise the parent's log level is
// used.
func (lggr *log) SubLogger(name string) Logger {
	combinedName := fmt.Sprintf("%s[%s]", lggr.name, name)
	logger := lggr.backend.Logger(combinedName)
	level := lggr.level
	// If levels exists and name is in the map, use that level.
	if lggr.levels != nil {
		if lvl, ok := lggr.levels[name]; ok {
			level = lvl
		}
	}
	logger.SetLevel(level)
	return &log{
		Logger:  logger,
		name:    combinedName,
		level:   level,
		levels:  lggr.levels,
		backend: lggr.backend,
	}
}

// StdOutLogger creates a Logger with the provided name with lvl as the log
// level and prints to standard out.
func StdOutLogger(name string, lvl slog.Level) Logger {
	backend := slog.NewBackend(os.Stdout)
	logger := backend.Logger(name)
	logger.SetLevel(lvl)
	return &log{
		Logger:  logger,
		name:    name,
		level:   lvl,
		backend: backend,
	}
}

// NewLoggerMaker parses the debug level string into a new *LoggerMaker. The
// debugLevel string can specify a single verbosity for the entire system:
// "trace", "debug", "info", "warn", "error", "critical", "off".
//
// Or the verbosity can be specified for individual subsystems, separating
// subsystems by commas and assigning each specifically, A command line might
// look like `--degublevel=CORE=debug,SWAP=trace`.
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
	return &log{
		Logger:  logger,
		name:    name,
		level:   lvl,
		levels:  lm.Levels,
		backend: lm.Backend,
	}
}

// Logger creates a logger with the provided name, using the log level for that
// name if it was set, otherwise the default log level. This differs from
// NewLogger, which does not look in the Level map for the name.
func (lm *LoggerMaker) Logger(name string) Logger {
	logger := lm.Backend.Logger(name)
	lvl := lm.bestLevel(name)
	logger.SetLevel(lvl)
	return &log{
		Logger:  logger,
		name:    name,
		level:   lvl,
		levels:  lm.Levels,
		backend: lm.Backend,
	}
}

// bestLevel takes a hierarchical list of logger names, least important to most
// important, and returns the best log level found in the Levels map, else the
// default.
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
