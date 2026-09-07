// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"fmt"
	"sync"
	"time"

	"decred.org/dcrdex/dex"
)

const defaultSlavePromotionDelay = 30 * time.Second

// queuedSignal is a signal queued for the reducer, with an optional reply
// channel: send sets it to receive the result, post leaves it nil.
type queuedSignal struct {
	signal meshSignal
	reply  chan signalResult
}

// signalResult is the synchronous reply returned by send.
type signalResult struct {
	handled bool
	err     error
	state   nodeState
	outcome signalOutcome
}

// controlLoopSink provides callbacks for lifecycle related events.
type controlLoopSink interface {
	handleEffect(effect)
	controlHalted(error)
}

// controlRun is a running control loop's two channels: the queue signals
// are sent on, and a done channel that tells a blocked sender the loop
// has exited.
type controlRun struct {
	queue   chan queuedSignal
	runDone <-chan struct{}
}

// controlLoop is the layer between the node and the pure
// reducer which updates the node's state. It is responsible
// for applying signals to the reducer sequentially, updating
// the node's state, and executing the effects returned by
// the reducer.
type controlLoop struct {
	sink                controlLoopSink
	log                 dex.Logger
	nodeID              string
	slavePromotionDelay time.Duration

	stateMtx       sync.RWMutex
	state          nodeState
	lastTransition time.Time
	// activeRun is non-nil when the loop is running.
	// There will only be one active run per process.
	activeRun *controlRun
}

func newControlLoop(log dex.Logger, nodeID string, sink controlLoopSink) *controlLoop {
	return &controlLoop{
		log:                 log,
		nodeID:              nodeID,
		slavePromotionDelay: defaultSlavePromotionDelay,
		sink:                sink,
		state:               nodeState{mode: modePending},
	}
}

// prepareRun sets up the queue before the loop goroutine starts. Once it
// returns, send and post are safe to call even if the loop hasn't been
// scheduled yet; signals just wait in the queue. Call it before run.
func (c *controlLoop) prepareRun(ctx context.Context) {
	if c.slavePromotionDelay <= 0 {
		c.slavePromotionDelay = defaultSlavePromotionDelay
	}

	c.stateMtx.Lock()
	c.state = nodeState{
		mode: modePending,
	}
	c.activeRun = &controlRun{
		queue:   make(chan queuedSignal, 16),
		runDone: ctx.Done(),
	}
	c.stateMtx.Unlock()
}

// teardown cleans up the control-loop variables after the loop exits.
func (c *controlLoop) teardown() {
	c.stateMtx.Lock()
	c.activeRun = nil
	c.stateMtx.Unlock()
}

// run starts the mesh control loop. startup resolves once startup reaches an
// established role (effectStartupResolved).
func (c *controlLoop) run(ctx context.Context, startup *readiness) {
	run := c.currentRun()
	if run == nil {
		return
	}
	defer c.teardown()

	for {
		select {
		case <-ctx.Done():
			return
		case queued := <-run.queue:
			c.handleSignal(queued, startup)
		}
	}
}

func replySignal(signal queuedSignal, result reduceResult, err error, state nodeState) {
	if signal.reply == nil {
		return
	}
	signal.reply <- signalResult{
		handled: result.handled,
		err:     err,
		state:   state,
		outcome: result.outcome,
	}
}

// handleSignal reduces one queued signal, commits the resulting state, and
// executes the reducer's effects.
func (c *controlLoop) handleSignal(queued queuedSignal, startup *readiness) {
	currState := c.currentState()
	sig := queued.signal

	result, err := reduceSignal(c.nodeID, c.slavePromotionDelay, currState, sig)
	c.logSignal(currState, result.next, sig, result.handled, err)

	if err != nil {
		replySignal(queued, result, err, currState)
		return
	}

	if !result.handled {
		replySignal(queued, result, nil, currState)
		return
	}

	nextState := result.next
	c.setState(nextState)
	c.logMeshStateUpdate(currState, nextState, sig)

	halt := c.applyReducerEffects(result.effects, startup)

	// Reply to the signal before calling the halt callback so a caller is never
	// torn down while still waiting on its own event.
	replySignal(queued, result, nil, nextState)

	if halt {
		c.sink.controlHalted(nextState.haltErr)
	}
}

// applyReducerEffects executes the reducer's effects in order.
// Some effects are directly handled by the control loop. Others
// are passed upstream to the node's effect handler.
func (c *controlLoop) applyReducerEffects(effects []effect, startup *readiness) (halt bool) {
	for _, eff := range effects {
		switch eff.(type) {
		case effectStartupResolved:
			startup.resolve(nil)
		case effectScheduleSlavePromotionCheck:
			c.scheduleSlavePromotionCheck()
		case effectHalt:
			halt = true
		default:
			c.sink.handleEffect(eff)
		}
	}
	return halt
}

func (c *controlLoop) logSignal(prev, next nodeState, sig meshSignal, handled bool, err error) {
	if err != nil {
		c.log.Errorf("Mesh signal failed: signal=%s state=%s err=%v",
			sig, prev, err)
		return
	}
	if handled {
		c.log.Tracef("Mesh signal handled: signal=%s prev=%s next=%s",
			sig, prev, next)
		return
	}
	c.log.Tracef("Mesh signal ignored: signal=%s state=%s",
		sig, prev)
}

func (c *controlLoop) logMeshStateUpdate(prev, next nodeState, sig meshSignal) {
	switch sig := sig.(type) {
	case streamCaughtUpSignal:
		c.log.Infof("Mesh event stream with peer %s caught up to target %s. Entered state %s.",
			meshPeerLogID(sig.conn), sig.target, next.mode)
	case streamFailedSignal:
		c.log.Warnf("Mesh event stream with peer %s failed: %v. Entered state %s.",
			meshPeerLogID(sig.conn), sig.err, next.mode)
	case connectionDisconnectedSignal:
		c.logConnectionDisconnected(prev, next, sig)
	case slavePromotionCheckSignal:
		c.log.Infof("Mesh peer remained disconnected for %v. Entered state %s.",
			c.slavePromotionDelay, next.mode)
	case plannedHandoffSignal:
		c.log.Infof("Mesh planned handoff received at frontier %s. Entered state %s.",
			sig.target, next.mode)
	case dialIncompatibleSignal:
		c.log.Warnf("Mesh peer incompatible: %v. Entered state %s.",
			sig.err, next.mode)
	case subscribeRejectedSignal:
		c.log.Warnf("Mesh subscribe rejected: %v. Entered state %s.",
			sig.err, next.mode)
	case terminalApplyFailureSignal:
		c.log.Criticalf("Mesh event apply failed terminally: %v. Entered state %s.",
			sig.err, next.mode)
	case masterReadySignal:
		c.log.Infof("Mesh master preparation complete. Entered state %s.", next.mode)
	case masterPreparationFailedSignal:
		c.log.Criticalf("Mesh master preparation failed: %v. Entered state %s.",
			sig.err, next.mode)
	}
}

func (c *controlLoop) logConnectionDisconnected(prev, next nodeState, sig connectionDisconnectedSignal) {
	switch {
	case prev.mode == modeEstablishedSlave && next.mode == modeSlaveNoMaster:
		c.log.Warnf("Mesh peer %s disconnected. Entered state %s. Will promote to master after %v if the peer remains disconnected.",
			meshPeerLogID(sig.conn), next.mode, c.slavePromotionDelay)
	case prev.mode != next.mode:
		c.log.Warnf("Mesh peer %s disconnected. Entered state %s.",
			meshPeerLogID(sig.conn), next.mode)
	case prev.mode.isAuthoritativeMaster() && next.mode.isAuthoritativeMaster():
		c.log.Warnf("Mesh peer %s disconnected. Remaining in state %s.",
			meshPeerLogID(sig.conn), next.mode)
	case prev.mode == modeEstablishedSlaveSyncing && next.mode == modePending:
		c.log.Warnf("Mesh peer %s disconnected during event stream sync. Entered state %s.",
			meshPeerLogID(sig.conn), next.mode)
	}
}

func meshPeerLogID(peerConn *nodeConn) string {
	if peerConn == nil {
		return "<nil>"
	}
	if peerConn.peerNodeID != "" {
		return peerConn.peerNodeID
	}
	return peerConn.String()
}

// send synchronously sends a signal to the control loop and waits for the
// result.
func (c *controlLoop) send(sig meshSignal) (signalResult, error) {
	run := c.currentRun()
	if run == nil {
		return signalResult{}, fmt.Errorf("mesh not running")
	}

	reply := make(chan signalResult, 1)

	select {
	case run.queue <- queuedSignal{signal: sig, reply: reply}:
	case <-run.runDone:
		return signalResult{}, fmt.Errorf("mesh stopped")
	}

	// Wait for the result. If the loop stops, return any reply
	// already queued.
	select {
	case res := <-reply:
		return res, nil
	case <-run.runDone:
		select {
		case res := <-reply:
			return res, nil
		default:
		}
		return signalResult{}, fmt.Errorf("mesh stopped")
	}
}

// post queues a signal without waiting for it to be processed.
func (c *controlLoop) post(sig meshSignal) error {
	run := c.currentRun()
	if run == nil {
		return fmt.Errorf("mesh not running")
	}

	select {
	case run.queue <- queuedSignal{signal: sig}:
		return nil
	case <-run.runDone:
		return fmt.Errorf("mesh stopped")
	}
}

// currentRun returns the active control run, or nil if none is active.
func (c *controlLoop) currentRun() *controlRun {
	c.stateMtx.RLock()
	defer c.stateMtx.RUnlock()
	return c.activeRun
}

// scheduleSlavePromotionCheck waits a certain period of time before posting a
// slavePromotionCheckSignal. This gives the master a period of time to return
// before promoting the slave to master, and possibly causing a split brain.
func (c *controlLoop) scheduleSlavePromotionCheck() {
	run := c.currentRun()
	if run == nil {
		return
	}

	go func() {
		timer := time.NewTimer(c.slavePromotionDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
			_ = c.post(slavePromotionCheckSignal{
				at: time.Now(),
			})
		case <-run.runDone:
		}
	}()
}
