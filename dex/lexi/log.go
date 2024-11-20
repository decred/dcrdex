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
func (log *badgerLoggerWrapper) Debugf(s string, a ...interface{}) {
	log.Tracef(s, a...)
}

func (log *badgerLoggerWrapper) Debug(a ...interface{}) {
	log.Trace(a...)
}

// Infof -> dex.Logger.Debugf
func (log *badgerLoggerWrapper) Infof(s string, a ...interface{}) {
	log.Debugf(s, a...)
}

func (log *badgerLoggerWrapper) Info(a ...interface{}) {
	log.Debug(a...)
}

// Warningf -> dex.Logger.Warnf
func (log *badgerLoggerWrapper) Warningf(s string, a ...interface{}) {
	log.Warnf(s, a...)
}

func (log *badgerLoggerWrapper) Warning(a ...interface{}) {
	log.Warn(a...)
}
