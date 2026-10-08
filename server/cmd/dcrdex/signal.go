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
	"sync"
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
	// interrupt signal or requestShutdown call. Any contexts created using
	// withShutdownCancel are canceled when this is closed.
	//
	// The channel close is guarded with shutdownOnce so repeated shutdown
	// requests from signals and internal callers are safe.
	shutdownSignal = make(chan struct{})
	shutdownOnce   sync.Once
)

// withShutdownCancel creates a copy of a context that is canceled whenever
// shutdown is invoked through an interrupt signal or an internal shutdown
// request.
func withShutdownCancel(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-shutdownSignal
		cancel()
	}()
	return ctx
}

// requestShutdown triggers shutdown for all contexts created with
// withShutdownCancel. It is safe to call multiple times.
func requestShutdown(reason string) {
	shutdownOnce.Do(func() {
		if reason != "" {
			log.Warnf("Shutdown requested: %s", reason)
		}
		close(shutdownSignal)
	})
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
	requestShutdown("")

	// Listen for any more shutdown signals and log that shutdown has already
	// been signaled.
	for {
		<-interruptChannel
		log.Info("Shutdown signaled. Already shutting down...")
	}
}
