// Copyright (c) 2018-2020, The Decred developers
// Copyright (c) 2013-2014, The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
)

// shutdownRequested checks if the Done channel of the given context has been
// closed. This could indicate cancellation, expiration, or deadline expiry. But
// when called for the context provided by withShutdownCancel, it indicates if
// shutdown has been requested (i.e. via os.Interrupt or requestShutdown).
func shutdownRequested(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

var (
	// shutdownSignal is closed whenever shutdown is invoked through an
	// interrupt signal. Any contexts created using withShutdownChannel are
	// canceled when this is closed.
	shutdownSignal = make(chan struct{})
)

// withShutdownCancel creates a copy of a context that is canceled whenever
// shutdown is invoked through an interrupt signal or from an JSON-RPC stop
// request.
func withShutdownCancel(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-shutdownSignal
		cancel()
	}()
	return ctx
}

// shutdownListener listens for shutdown requests and cancels all contexts
// created from withShutdownCancel. This function never returns and is intended
// to be spawned in a new goroutine.
func shutdownListener() {
	interruptChannel := make(chan os.Signal, 1)
	signal.Notify(interruptChannel, os.Interrupt)

	// Listen for the initial shutdown signal.
	sig := <-interruptChannel
	fmt.Printf("Received signal (%s). Shutting down...\n", sig)

	// Cancel all contexts created from withShutdownCancel.
	close(shutdownSignal)

	// Listen for any more shutdown signals and log that shutdown has already
	// been signaled.
	for {
		<-interruptChannel
		log.Info("Shutdown signaled. Already shutting down...")
	}
}
