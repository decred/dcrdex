// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
)

// shutdownSignaled is closed whenever shutdown is invoked through an interrupt
// signal. Any contexts created using withShutdownChannel are cancelled when
// this is closed.
var shutdownSignaled = make(chan struct{})

// WithShutdownCancel creates a copy of a context that is cancelled whenever
// shutdown is invoked through an interrupt signal.
func withShutdownCancel(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-shutdownSignaled
		cancel()
	}()
	return ctx
}

// ShutdownListener listens for shutdown requests and cancels all contexts
// created from withShutdownCancel. This function never returns and is intended
// to be spawned in a new goroutine.
func shutdownListener() {
	interruptChannel := make(chan os.Signal, 1)
	// Only accept a single CTRL+C.
	signal.Notify(interruptChannel, os.Interrupt)

	// Listen for the initial shutdown signal
	sig := <-interruptChannel
	fmt.Printf("\nReceived signal (%s). Shutting down...\n", sig)

	// Cancel all contexts created from withShutdownCancel.
	close(shutdownSignaled)

	// Listen for any more shutdown signals and log that shutdown has already
	// been signaled.
	for {
		<-interruptChannel
		fmt.Println("Shutdown signaled. Already shutting down...")
	}
}
