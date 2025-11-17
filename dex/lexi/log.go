package lexi

import (
	"decred.org/dcrdex/dex"
	"github.com/dgraph-io/badger"
)

// badgerLoggerWrapper wraps dex.Logger and translates Warnf to Warningf to
// satisfy badger.Logger. It also lowers the log level of Infof to Debugf
// and Debugf to Tracef.
type badgerLoggerWrapper struct {
	dex.Logger
}

var _ badger.Logger = (*badgerLoggerWrapper)(nil)

// Debugf -> dex.Logger.Tracef
func (log *badgerLoggerWrapper) Debugf(s string, a ...any) {
	log.Tracef(s, a...)
}

func (log *badgerLoggerWrapper) Debug(a ...any) {
	log.Trace(a...)
}

// Infof -> dex.Logger.Debugf
func (log *badgerLoggerWrapper) Infof(s string, a ...any) {
	log.Debugf(s, a...)
}

func (log *badgerLoggerWrapper) Info(a ...any) {
	log.Debug(a...)
}

// Warningf -> dex.Logger.Warnf
func (log *badgerLoggerWrapper) Warningf(s string, a ...any) {
	log.Warnf(s, a...)
}

func (log *badgerLoggerWrapper) Warning(a ...any) {
	log.Warn(a...)
}
