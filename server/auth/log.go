// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"github.com/decred/slog"
)

// log is a logger that is initialized with no output filters. This means the
// package will not perform any logging by default until the caller requests it.
var log = slog.Disabled

// DisableLog disables all library log output.  Logging output is disabled
// by default until UseLogger is called.
func DisableLog() {
	log = slog.Disabled
}

// UseLogger uses a specified Logger to output package logging info.
func UseLogger(logger slog.Logger) {
	log = logger
}
