// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/server/db"
)

func unexpectedControlEffect(eff effect) {
	panic(fmt.Sprintf("unexpected reducer effect %T", eff))
}

func unexpectedHalted(error) {
	panic("unexpected halt callback")
}

type testControlSink struct {
	effect func(effect)
	halted func(error)
}

func (s *testControlSink) handleEffect(eff effect) {
	if s.effect != nil {
		s.effect(eff)
	}
}

func (s *testControlSink) controlHalted(err error) {
	if s.halted != nil {
		s.halted(err)
	}
}

func newControlLoopWithFuncs(log dex.Logger, nodeID string, effectFn func(effect), haltedFn func(error)) *controlLoop {
	return newControlLoop(log, nodeID, &testControlSink{effect: effectFn, halted: haltedFn})
}

func newTestControlLoop(state nodeState) *controlLoop {
	c := newControlLoopWithFuncs(dex.Disabled, "node-a", unexpectedControlEffect, unexpectedHalted)
	c.setState(state)
	return c
}

func newQueuedTestControlLoop(state nodeState, queueSize int) *controlLoop {
	c := newTestControlLoop(state)
	c.stateMtx.Lock()
	c.activeRun = &controlRun{
		queue:   make(chan queuedSignal, queueSize),
		runDone: make(chan struct{}),
	}
	c.stateMtx.Unlock()
	return c
}

// testQueue exposes the active run's signal queue to tests.
func (c *controlLoop) testQueue() chan queuedSignal {
	run := c.currentRun()
	if run == nil {
		return nil
	}
	return run.queue
}

type controlSendOutcome struct {
	res signalResult
	err error
}

func TestControlLoopBecameMasterCallback(t *testing.T) {
	const delay = time.Second
	at := time.Unix(1_700_000_000, 0)
	calls := 0
	control := newControlLoopWithFuncs(dex.Disabled, "node-a", func(eff effect) {
		switch eff.(type) {
		case effectBecameMaster:
			calls++
		case effectFailPendingCommands:
		default:
			unexpectedControlEffect(eff)
		}
	}, unexpectedHalted)
	control.slavePromotionDelay = delay
	control.setState(nodeState{
		mode:             modeSlaveNoMaster,
		peerDisconnected: at,
	})

	control.handleSignal(queuedSignal{
		signal: slavePromotionCheckSignal{at: at.Add(delay)},
	}, newReadiness())

	if control.currentMode() != modePreparingMaster {
		t.Fatalf("current mode = %s, want %s", control.currentMode(), modePreparingMaster)
	}
	if calls != 1 {
		t.Fatalf("becameMaster calls = %d, want 1", calls)
	}

	control.handleSignal(queuedSignal{
		signal: masterReadySignal{},
	}, newReadiness())
	if control.currentMode() != modeEstablishedMaster {
		t.Fatalf("current mode after master ready = %s, want %s", control.currentMode(), modeEstablishedMaster)
	}
	if calls != 1 {
		t.Fatalf("becameMaster calls after master ready = %d, want 1", calls)
	}

	control.handleSignal(queuedSignal{
		signal: slavePromotionCheckSignal{at: at.Add(2 * delay)},
	}, newReadiness())
	if calls != 1 {
		t.Fatalf("becameMaster calls after ignored event = %d, want 1", calls)
	}
}

func TestControlLoopHandleMeshEventOrdering(t *testing.T) {
	conn := testSession("node-b", "node-a")
	reply := make(chan signalResult, 1)
	var order []string
	var control *controlLoop
	control = newControlLoopWithFuncs(dex.Disabled, "node-a", func(eff effect) {
		mode := control.currentState().mode
		if mode != modePreparingMaster && mode != modeEstablishedMaster {
			t.Fatalf("state not committed before effect %T: %s", eff, control.currentState())
		}
		if _, ok := eff.(effectBecameMaster); ok && mode != modePreparingMaster {
			t.Fatalf("state not committed before becameMaster: %s", control.currentState())
		}
		order = append(order, reflect.TypeOf(eff).Name())
	}, unexpectedHalted)

	startup := newReadiness()
	control.handleSignal(queuedSignal{
		signal: testHandshakeResolvedSignal(conn, roleUnknown, progressEqual),
		reply:  reply,
	}, startup)

	if control.currentMode() != modePreparingMaster {
		t.Fatalf("current mode = %s, want %s", control.currentMode(), modePreparingMaster)
	}
	select {
	case <-startup.resolved():
		t.Fatal("startup resolved before master preparation completed")
	default:
	}
	select {
	case res := <-reply:
		if res.err != nil || !res.handled || res.state.mode != modePreparingMaster {
			t.Fatalf("reply = %+v, want handled preparing master", res)
		}
	default:
		t.Fatal("missing synchronous reply")
	}
	control.handleSignal(queuedSignal{
		signal: masterReadySignal{},
	}, startup)
	if control.currentMode() != modeEstablishedMaster {
		t.Fatalf("current mode = %s, want %s", control.currentMode(), modeEstablishedMaster)
	}
	select {
	case <-startup.resolved():
	default:
		t.Fatal("startup not resolved after master ready")
	}
	wantOrder := []string{
		"effectBecameMaster",
		"effectWatchConn",
		"effectPeerClientEndpointChanged",
		"effectFailPendingCommands",
	}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("order = %v, want %v", order, wantOrder)
	}
}

func TestControlLoopHaltCallback(t *testing.T) {
	t.Run("startup halt preserves reducer result", func(t *testing.T) {
		var (
			effectMtx sync.Mutex
			effects   []string
		)
		startup := newReadiness()
		runCtx, cancel := context.WithCancel(context.Background())
		control := newControlLoopWithFuncs(dex.Disabled, "node-a", func(eff effect) {
			effectMtx.Lock()
			effects = append(effects, reflect.TypeOf(eff).Name())
			effectMtx.Unlock()
		}, func(error) {
			cancel()
		})
		var wg sync.WaitGroup
		control.prepareRun(runCtx)
		wg.Add(1)
		go func() {
			defer wg.Done()
			control.run(runCtx, startup)
		}()
		defer func() {
			cancel()
			wg.Wait()
		}()

		conn := testSession("node-b", "node-a")
		sendDone := make(chan controlSendOutcome, 1)
		go func() {
			res, err := control.send(testHandshakeResolvedSignal(conn, roleSlave, progressDiverged))
			sendDone <- controlSendOutcome{res: res, err: err}
		}()

		select {
		case <-runCtx.Done():
		case <-time.After(time.Second):
			t.Fatal("control loop was not canceled after halt")
		}

		err := (&node{control: control}).waitForStartup(runCtx, startup)
		if err == nil || !strings.Contains(err.Error(), "halting after handshake") {
			t.Fatalf("startup error = %v, want handshake halt error", err)
		}
		if control.currentState().mode != modeHalted {
			t.Fatalf("state mode = %s, want halted", control.currentState().mode)
		}
		var outcome controlSendOutcome
		select {
		case outcome = <-sendDone:
		case <-time.After(time.Second):
			t.Fatal("send did not return after halt")
		}
		if outcome.err != nil {
			t.Fatalf("send error = %v, want reducer result", outcome.err)
		}
		if !outcome.res.handled || outcome.res.state.mode != modeHalted {
			t.Fatalf("send result = %+v, want handled halted state", outcome.res)
		}
		if outcome.res.state.haltErr == nil || !strings.Contains(outcome.res.state.haltErr.Error(), "halting after handshake") {
			t.Fatalf("send halt error = %v, want handshake halt error", outcome.res.state.haltErr)
		}

		effectMtx.Lock()
		gotEffects := append([]string(nil), effects...)
		effectMtx.Unlock()
		wantEffects := []string{"effectDisconnect"}
		if !reflect.DeepEqual(gotEffects, wantEffects) {
			t.Fatalf("effects = %v, want %v", gotEffects, wantEffects)
		}
	})

	t.Run("runtime halt replies before callback", func(t *testing.T) {
		haltErr := errors.New("apply failed")
		reply := make(chan signalResult, 1)
		runCtx, cancel := context.WithCancel(context.Background())

		callbackCalled := false
		control := newControlLoopWithFuncs(dex.Disabled, "node-a", unexpectedControlEffect, func(err error) {
			callbackCalled = true
			if !errors.Is(err, haltErr) {
				t.Fatalf("halt callback error = %v, want %v", err, haltErr)
			}
			if len(reply) != 1 {
				t.Fatalf("halt callback ran before synchronous reply was sent")
			}
			cancel()
			select {
			case <-runCtx.Done():
			default:
				t.Fatalf("halt callback did not cancel run context")
			}
		})
		control.prepareRun(runCtx)
		defer control.teardown()
		control.setState(nodeState{mode: modeEstablishedMaster})

		startup := newReadiness()
		control.handleSignal(queuedSignal{
			signal: terminalApplyFailureSignal{err: haltErr, at: time.Now()},
			reply:  reply,
		}, startup)
		select {
		case <-startup.resolved():
			t.Fatalf("startup reported for runtime halt")
		default:
		}

		if !callbackCalled {
			t.Fatalf("halt callback was not called")
		}
		select {
		case res := <-reply:
			if res.err != nil || !res.handled || res.state.mode != modeHalted {
				t.Fatalf("reply = %+v, want handled halted state", res)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing synchronous reply")
		}
	})
}

func TestControlLoopSendReturnsStoppedIfMeshStopsBeforeReply(t *testing.T) {
	control := newQueuedTestControlLoop(nodeState{mode: modePending}, 1)
	runDone := make(chan struct{})
	control.stateMtx.Lock()
	control.activeRun.runDone = runDone
	control.stateMtx.Unlock()

	sendDone := make(chan controlSendOutcome, 1)
	go func() {
		res, err := control.send(slavePromotionCheckSignal{})
		sendDone <- controlSendOutcome{res: res, err: err}
	}()

	var queued queuedSignal
	select {
	case queued = <-control.testQueue():
	case <-time.After(time.Second):
		t.Fatal("send did not enqueue event")
	}
	if queued.reply == nil {
		t.Fatal("send queued event without reply channel")
	}

	close(runDone)
	select {
	case outcome := <-sendDone:
		if outcome.err == nil || !strings.Contains(outcome.err.Error(), "mesh stopped") {
			t.Fatalf("send error = %v, want mesh stopped", outcome.err)
		}
	case <-time.After(time.Second):
		t.Fatal("send did not return after mesh stopped")
	}
}

func TestControlLoopPromotionTimer(t *testing.T) {
	becameMaster := make(chan struct{}, 1)
	control := newControlLoopWithFuncs(dex.Disabled, "node-b", func(eff effect) {
		if _, ok := eff.(effectBecameMaster); !ok {
			return
		}
		select {
		case becameMaster <- struct{}{}:
		default:
		}
	}, unexpectedHalted)
	control.slavePromotionDelay = 20 * time.Millisecond
	startup := newReadiness()
	runCtx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	control.prepareRun(runCtx)
	wg.Add(1)
	go func() {
		defer wg.Done()
		control.run(runCtx, startup)
	}()
	defer func() {
		cancel()
		wg.Wait()
	}()

	conn := testSession("node-a", "node-b")
	if res, err := control.send(testHandshakeResolvedSignal(conn, roleUnknown, progressEqual)); err != nil || res.err != nil {
		t.Fatalf("handshake send result = %+v err=%v", res, err)
	}
	if control.currentMode() != modeEstablishedSlave {
		t.Fatalf("mode after handshake = %s, want %s", control.currentMode(), modeEstablishedSlave)
	}
	if res, err := control.send(connectionDisconnectedSignal{conn: conn, at: time.Now()}); err != nil || res.err != nil {
		t.Fatalf("disconnect send result = %+v err=%v", res, err)
	}
	waitForCondition(t, func() bool {
		return control.currentMode() == modePreparingMaster
	}, "slave promotion timer")
	select {
	case <-becameMaster:
	case <-time.After(time.Second):
		t.Fatal("missing becameMaster callback")
	}
}

func TestControlLoopSendPostLifecycle(t *testing.T) {
	control := newControlLoopWithFuncs(dex.Disabled, "node-a", func(effect) {}, unexpectedHalted)
	if _, err := control.send(slavePromotionCheckSignal{}); err == nil || !strings.Contains(err.Error(), "mesh not running") {
		t.Fatalf("send before start error = %v, want mesh not running", err)
	}
	if err := control.post(slavePromotionCheckSignal{}); err == nil || !strings.Contains(err.Error(), "mesh not running") {
		t.Fatalf("post before start error = %v, want mesh not running", err)
	}

	startup := newReadiness()
	runCtx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	control.prepareRun(runCtx)
	wg.Add(1)
	go func() {
		defer wg.Done()
		control.run(runCtx, startup)
	}()
	defer func() {
		cancel()
		wg.Wait()
	}()

	conn := testSession("node-b", "node-a")
	res, err := control.send(testHandshakeResolvedSignal(conn, roleUnknown, progressEqual))
	if err != nil || res.err != nil || !res.handled {
		t.Fatalf("send while running result = %+v err=%v", res, err)
	}
	if err := control.post(streamCaughtUpSignal{conn: conn, target: &db.EventLogPosition{}}); err != nil {
		t.Fatalf("post while running error = %v", err)
	}

	cancel()
	wg.Wait()
	if _, err := control.send(slavePromotionCheckSignal{}); err == nil {
		t.Fatalf("send after stop error = nil, want error")
	}
	if err := control.post(slavePromotionCheckSignal{}); err == nil {
		t.Fatalf("post after stop error = nil, want error")
	}
}
