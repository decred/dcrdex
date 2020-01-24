package dex

import (
	"context"
	"sync"
)

// Runner is satisfied by DEX subsystems, which must start any of their
// goroutines via the Run method.
type Runner interface {
	Run(ctx context.Context) //error
}

// StartStopWaiter wraps a Runner, providing the non-blocking Start and Stop
// methods, and the blocking WaitForShutdown method.
type StartStopWaiter struct {
	runner Runner
	wg     sync.WaitGroup
	cancel context.CancelFunc
	ctx    context.Context
}

// NewStartStopWaiter creates a StartStopWaiter from a Runner.
func NewStartStopWaiter(runner Runner) *StartStopWaiter {
	return &StartStopWaiter{
		runner: runner,
	}
}

// Start launches the Runner in a goroutine. Start will return immediately. Use
// Stop to signal the Runner to stop, followed by WaitForShutdown to allow
// shutdown to complete.
func (ssw *StartStopWaiter) Start(ctx context.Context) {
	ssw.ctx, ssw.cancel = context.WithCancel(ctx)
	ssw.wg.Add(1)
	go func() {
		ssw.runner.Run(ssw.ctx)
		ssw.wg.Done()
	}()
}

// Stop signals the Runner to quit. It is not blocking; use WaitForShutdown
// after Stop to allow the Runner to return.
func (ssw *StartStopWaiter) Stop() {
	ssw.cancel()
}

// On will be true until the Runner Context is canceled.
func (ssw *StartStopWaiter) On() bool {
	return ssw.ctx.Err() == nil
}

// WaitForShutdown blocks until the Runner has returned in response to Stop.
func (ssw *StartStopWaiter) WaitForShutdown() {
	ssw.wg.Wait()
}
