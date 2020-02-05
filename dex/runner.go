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
	Run(ctx context.Context) //error
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
		ssw.wg.Done()
	}()
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
// a connection error, and a channel that can be used to wait on shutdown after
// context cancellation.
type Connector interface {
	Connect(ctx context.Context) (error, *sync.WaitGroup)
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

// Start connects the Connector, and returns any initial connection error. Use
// Disconnect to shut down the Connector.
func (c *ConnectionMaster) Connect(ctx context.Context) (err error) {
	c.init(ctx)
	c.mtx.Lock()
	err, c.wg = c.connector.Connect(c.ctx)
	c.mtx.Unlock()
	return err
}

// Disconnect closes the connection and waits for shutdown.
func (c *ConnectionMaster) Disconnect() {
	c.mtx.RLock()
	c.cancel()
	defer c.mtx.RUnlock()
	c.wg.Wait()
}
