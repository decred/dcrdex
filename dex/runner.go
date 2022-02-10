// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
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
	connector Connector
	cancel    context.CancelFunc
	done      atomic.Value // chan struct{}
}

// NewConnectionMaster creates a new ConnectionMaster. The Connect method should
// be used before Disconnect. The On, Done, and Wait methods may be used at any
// time. However, prior to Connect, Wait and Done immediately return and signal
// completion, respectively.
func NewConnectionMaster(c Connector) *ConnectionMaster {
	return &ConnectionMaster{
		connector: c,
	}
}

// Connect connects the Connector, and returns any initial connection error. Use
// Disconnect to shut down the Connector. Even if Connect returns a non-nil
// error, On may report true until Disconnect is called. You would use Connect
// if the wrapped Connector has a reconnect loop to continually attempt to
// establish a connection even if the initial attempt fails. Use ConnectOnce if
// the Connector should be given one chance to connect before being considered
// not to be "on". If the ConnectionMaster is discarded on error, it is not
// important which method is used.
func (c *ConnectionMaster) Connect(ctx context.Context) (err error) {
	if c.On() { // probably a bug in the consumer
		return errors.New("already running")
	}

	// Attempt to start the Connector.
	ctx, cancel := context.WithCancel(ctx)
	wg, err := c.connector.Connect(ctx)
	if wg == nil {
		cancel() // no context leak
		return fmt.Errorf("connect failure: %w", err)
	}
	// NOTE: A non-nil error currently does not indicate that the Connector is
	// not running, only that the initial connection attempt has failed. As long
	// as the WaitGroup is non-nil we need to wait on it. We return the error so
	// that the caller may decide to stop it or wait (see ConnectOnce).

	// It's running, enable Disconnect.
	c.cancel = cancel // caller should synchronize Connect/Disconnect calls

	// Done and On may be checked at any time.
	done := make(chan struct{})
	c.done.Store(done)

	go func() { // capture the local variables
		wg.Wait()
		cancel() // if the Connector just died on its own, don't leak the context
		close(done)
	}()

	return err
}

// ConnectOnce is like Connect, but on error the internal status is updated so
// that the On method returns false. This method may be used if an error from
// the Connector is terminal. The caller may also use Connect if they cancel the
// parent context or call Disconnect.
func (c *ConnectionMaster) ConnectOnce(ctx context.Context) (err error) {
	if err = c.Connect(ctx); err != nil {
		// If still "On", disconnect.
		// c.Disconnect() // no-op if not "On"
		if c.cancel != nil {
			c.cancel()
			<-c.done.Load().(chan struct{}) // wait for Connector
		}
	}
	return err
}

// Done returns a channel that is closed when the Connector's WaitGroup is done.
// If called before Connect, a closed channel is returned.
func (c *ConnectionMaster) Done() <-chan struct{} {
	done, ok := c.done.Load().(chan struct{})
	if ok {
		return done
	}
	done = make(chan struct{})
	close(done)
	return done
}

// On indicates if the Connector is running. This returns false if never
// connected, or if the Connector has completed shut down.
func (c *ConnectionMaster) On() bool {
	select {
	case <-c.Done():
		return false
	default:
		return true
	}
}

// Wait waits for the the Connector to shut down. It returns immediately if
// Connect has not been called yet.
func (c *ConnectionMaster) Wait() {
	<-c.Done() // let the anon goroutine from Connect return
}

// Disconnect closes the connection and waits for shutdown. This must not be
// used before or concurrently with Connect.
func (c *ConnectionMaster) Disconnect() {
	if !c.On() {
		return
	}
	c.cancel()
	c.Wait()
}
