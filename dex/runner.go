// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"context"
	"sync"
)

// contextManager is used to manage a context and its cancellation function.
type contextManager struct {
	mtx    sync.RWMutex
	cancel context.CancelFunc
	ctx    context.Context
}

// init uses the passed context to create and save a child context and
// its cancellation function.
func (cm *contextManager) init(ctx context.Context) {
	cm.mtx.Lock()
	defer cm.mtx.Unlock()
	cm.ctx, cm.cancel = context.WithCancel(ctx)
}

// On will be true until the context is canceled.
func (cm *contextManager) On() bool {
	cm.mtx.RLock()
	defer cm.mtx.RUnlock()
	return cm.ctx != nil && cm.ctx.Err() == nil
}

// Runner is satisfied by DEX subsystems, which must start any of their
// goroutines via the Run method.
type Runner interface {
	Run(ctx context.Context)
	// Ready() <-chan struct{}
}

// StartStopWaiter wraps a Runner, providing the non-blocking Start and Stop
// methods, and the blocking WaitForShutdown method.
type StartStopWaiter struct {
	contextManager
	wg     sync.WaitGroup
	runner Runner
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
	ssw.init(ctx)
	ssw.wg.Add(1)
	go func() {
		ssw.runner.Run(ssw.ctx)
		ssw.cancel() // in case it stopped on its own
		ssw.wg.Done()
	}()
	// TODO: do <-ssw.runner.Ready()
}

// WaitForShutdown blocks until the Runner has returned in response to Stop.
func (ssw *StartStopWaiter) WaitForShutdown() {
	ssw.wg.Wait()
}

// Stop cancels the context.
func (ssw *StartStopWaiter) Stop() {
	ssw.mtx.RLock()
	ssw.cancel()
	ssw.mtx.RUnlock()
}

// Connector is any type that implements the Connect method, which will return
// a connection error, and a WaitGroup that can be waited on at Disconnection.
type Connector interface {
	Connect(ctx context.Context) (*sync.WaitGroup, error)
}

// ConnectionMaster manages a Connector.
type ConnectionMaster struct {
	contextManager
	wg        *sync.WaitGroup
	connector Connector
}

// NewConnectionMaster is the constructor for a new ConnectionMaster.
func NewConnectionMaster(c Connector) *ConnectionMaster {
	return &ConnectionMaster{
		connector: c,
	}
}

// Connect connects the Connector, and returns any initial connection error. Use
// Disconnect to shut down the Connector.
func (c *ConnectionMaster) Connect(ctx context.Context) (err error) {
	c.init(ctx)
	c.mtx.Lock()
	c.wg, err = c.connector.Connect(c.ctx)
	c.mtx.Unlock()
	return err
}

// Disconnect closes the connection and waits for shutdown.
func (c *ConnectionMaster) Disconnect() {
	c.cancel()
	c.mtx.RLock()
	c.wg.Wait()
	c.mtx.RUnlock()
}

// Wait waits for the the WaitGroup returned by Connect.
func (c *ConnectionMaster) Wait() {
	c.wg.Wait()
	c.cancel() // if not called from Disconnect, would leak context
}
