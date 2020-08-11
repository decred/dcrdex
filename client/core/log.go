// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package core

import (
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db/bolt"
	orderbook "decred.org/dcrdex/client/order"
	"decred.org/dcrdex/dex"
)

// log is a logger that is initialized with no output filters. This means the
// package will not perform any logging by default until the caller requests it.
var log = dex.Disabled

// DisableLog disables all library log output.  Logging output is disabled
// by default until UseLogger is called.
func DisableLog() {
	log = dex.Disabled
}

// UseLogger uses a specified Logger to output package logging info.
func UseLoggerMaker(maker *dex.LoggerMaker) {
	log = maker.Logger("CORE")
	orderbook.UseLogger(maker.Logger("ORDBOOK"))
	comms.UseLogger(maker.Logger("COMMS"))
	bolt.UseLogger(maker.Logger("DB"))
}
