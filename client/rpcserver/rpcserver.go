// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package rpcserver

import (
	"context"

	"decred.org/dcrdex/client/core"
	"github.com/decred/slog"
)

var log slog.Logger

func Run(ctx context.Context, core *core.Core, addr string, logger slog.Logger) {
	log = logger
	log.Infof("RPC server running at %s", addr)
	<-ctx.Done()
	log.Infof("RPC server off")
}
