package mesh

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/db"
)

type meshConnectResult struct {
	wg  *sync.WaitGroup
	err error
}

var integrationTestLogger dex.Logger = dex.Disabled

type runningMeshNode struct {
	node *node

	cancel context.CancelFunc
	result chan meshConnectResult

	wg              *sync.WaitGroup
	connectReturned bool
}

func reserveTCPAddr(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	return l.Addr().String()
}

type integrationNodeConfig struct {
	listenAddr, peerAddr string
	compat               *CompatSnapshot
	eventLogReader       db.EventLogReader
	initialSeq           uint64
	cmdHandler           func(context.Context, string, CommandRequest) *msgjson.Error
	resultHandler        func(string, json.RawMessage)
	proxyHandler         func(context.Context, *ClientProxyMessage) error
	eventHandler         func(context.Context, *eventEnvelope) error

	// A nil lifecycle reports master readiness automatically. Supplying hooks
	// lets the test control readiness.
	lifecycle *lifecycleHooks
}

func newIntegrationNode(t *testing.T, cfg integrationNodeConfig) *node {
	t.Helper()
	var node *node
	eventLogReader := cfg.eventLogReader
	lifecycle := lifecycleHooks{}
	if cfg.lifecycle != nil {
		lifecycle = *cfg.lifecycle
	} else {
		lifecycle.becameMaster = func() {
			go func() {
				if node != nil {
					_ = node.notifyMasterReady()
				}
			}()
		}
	}

	if lifecycle.becameMaster == nil {
		lifecycle.becameMaster = func() {}
	}
	if lifecycle.peerClientEndpointChanged == nil {
		lifecycle.peerClientEndpointChanged = func(string, []byte) {}
	}
	if lifecycle.failPendingCommands == nil {
		lifecycle.failPendingCommands = func(string) {}
	}
	if lifecycle.halted == nil {
		lifecycle.halted = func(error) {}
	}

	initialFrontier := &db.EventLogPosition{Seq: cfg.initialSeq}
	if eventLogReader == nil {
		eventLogReader = &testEventLogReader{frontier: initialFrontier}
	} else {
		frontier, err := eventLogReader.EventLogFrontier(context.Background())
		if err != nil {
			t.Fatalf("initial frontier: %v", err)
		}
		if frontier != nil {
			initialFrontier = frontier
		}
	}

	var err error
	node, err = newNode(&nodeConfig{
		dataDir:         t.TempDir(),
		listenAddr:      cfg.listenAddr,
		peerAddr:        cfg.peerAddr,
		compat:          cfg.compat,
		dexPrivKey:      testPrivKey(),
		noTLS:           true,
		logger:          integrationTestLogger,
		eventLogReader:  eventLogReader,
		snapshotStore:   &fakeSnapshotStore{},
		initialFrontier: initialFrontier,
		app: &testMeshApplication{
			commandForward: cfg.cmdHandler,
			commandResult:  cfg.resultHandler,
			clientProxy:    cfg.proxyHandler,
			event:          cfg.eventHandler,
		},
		lifecycle: lifecycle,
	})
	if err != nil {
		t.Fatalf("newNode(%s): %v", cfg.listenAddr, err)
	}
	// These tests have no state loaders; open the events gate immediately,
	// as the service does once its loaders complete.
	node.notifyReadyForEvents()

	return node
}

func startIntegrationNode(t *testing.T, node *node) *runningMeshNode {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	run := &runningMeshNode{
		node:   node,
		cancel: cancel,
		result: make(chan meshConnectResult, 1),
	}

	go func() {
		wg, err := node.connect(ctx)
		run.result <- meshConnectResult{wg: wg, err: err}
	}()

	return run
}

func (r *runningMeshNode) waitForConnectResult(t *testing.T, timeout time.Duration) meshConnectResult {
	t.Helper()

	select {
	case res := <-r.result:
		r.connectReturned = true
		r.wg = res.wg
		return res
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for Connect to return; state=%s", r.node.control.currentState())
		return meshConnectResult{}
	}
}

func (r *runningMeshNode) waitForStartup(t *testing.T) {
	t.Helper()

	res := r.waitForConnectResult(t, 5*time.Second)
	if res.err != nil {
		t.Fatalf("Connect error: %v", res.err)
	}
	if res.wg == nil {
		t.Fatal("Connect returned nil waitgroup")
	}
}

func (r *runningMeshNode) assertStartupPending(t *testing.T, timeout time.Duration) {
	t.Helper()

	select {
	case res := <-r.result:
		r.connectReturned = true
		r.wg = res.wg
		t.Fatalf("Connect returned early: wg=%v err=%v", res.wg, res.err)
	case <-time.After(timeout):
	}
}

func (r *runningMeshNode) shutdown(t *testing.T) {
	t.Helper()

	r.cancel()

	if !r.connectReturned {
		res := r.waitForConnectResult(t, 5*time.Second)
		if res.err != nil || res.wg == nil {
			return
		}
	}
	if r.wg == nil {
		return
	}

	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for mesh node shutdown")
	}
}

func waitForMeshCondition(t *testing.T, desc string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", desc)
}

func waitForComplementaryModes(t *testing.T, a, b *node) (master, slave *node) {
	t.Helper()

	var pairedSince time.Time
	var aConnID, bConnID uint64
	waitForMeshCondition(t, "complementary master/slave modes with paired active link", func() bool {
		aState := a.control.currentState()
		bState := b.control.currentState()
		modesReady := (aState.mode == modeEstablishedMaster && bState.mode == modeEstablishedSlave) ||
			(aState.mode == modeEstablishedSlave && bState.mode == modeEstablishedMaster)
		if !modesReady || aState.activeConn == nil || bState.activeConn == nil ||
			aState.activeConn.initiatorNodeID != bState.activeConn.initiatorNodeID {
			pairedSince = time.Time{}
			return false
		}

		if aID, bID := aState.activeConn.ID(), bState.activeConn.ID(); pairedSince.IsZero() || aID != aConnID || bID != bConnID {
			aConnID, bConnID = aID, bID
			pairedSince = time.Now()
			return false
		}
		return time.Since(pairedSince) >= 100*time.Millisecond
	})

	if a.control.currentMode() == modeEstablishedMaster {
		return a, b
	}
	return b, a
}

func newMeshPair(t *testing.T, aCompat, bCompat *CompatSnapshot) (*node, *node) {
	t.Helper()

	aListen := reserveTCPAddr(t)
	bListen := reserveTCPAddr(t)

	aPeer := "ws://" + bListen + meshWSPath
	bPeer := "ws://" + aListen + meshWSPath

	nodeA := newIntegrationNode(t, integrationNodeConfig{
		listenAddr: aListen,
		peerAddr:   aPeer,
		compat:     aCompat,
	})
	nodeB := newIntegrationNode(t, integrationNodeConfig{
		listenAddr: bListen,
		peerAddr:   bPeer,
		compat:     bCompat,
	})

	return nodeA, nodeB
}

func TestMeshNodesEstablishMasterSlave(t *testing.T) {
	nodeA, nodeB := newMeshPair(t, testCompatSnapshot(t), testCompatSnapshot(t))

	runA := startIntegrationNode(t, nodeA)
	runB := startIntegrationNode(t, nodeB)
	t.Cleanup(func() {
		runA.shutdown(t)
		runB.shutdown(t)
	})

	runA.waitForStartup(t)
	runB.waitForStartup(t)

	master, slave := waitForComplementaryModes(t, nodeA, nodeB)
	if !master.control.hasPeerConnection() {
		t.Fatal("master has no active peer connection")
	}
	if !slave.control.hasPeerConnection() {
		t.Fatal("slave has no active peer connection")
	}
}

func TestMeshConnectWaitsForPeerUntilCanceled(t *testing.T) {
	listenAddr := reserveTCPAddr(t)
	peerAddr := "ws://" + reserveTCPAddr(t) + meshWSPath

	node := newIntegrationNode(t, integrationNodeConfig{
		listenAddr: listenAddr,
		peerAddr:   peerAddr,
		compat:     testCompatSnapshot(t),
	})
	run := startIntegrationNode(t, node)

	run.assertStartupPending(t, 200*time.Millisecond)

	run.cancel()
	res := run.waitForConnectResult(t, 5*time.Second)
	if res.err == nil || !strings.Contains(res.err.Error(), "mesh stopped during startup") {
		t.Fatalf("Connect error = %v, want mesh stopped during startup", res.err)
	}
	if res.wg != nil {
		done := make(chan struct{})
		go func() {
			res.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for mesh node shutdown")
		}
	}
}

func TestMeshNodeStartsAfterPeerAppears(t *testing.T) {
	nodeA, nodeB := newMeshPair(t, testCompatSnapshot(t), testCompatSnapshot(t))

	runA := startIntegrationNode(t, nodeA)
	t.Cleanup(func() {
		runA.shutdown(t)
	})

	runA.assertStartupPending(t, 200*time.Millisecond)

	runB := startIntegrationNode(t, nodeB)
	t.Cleanup(func() {
		runB.shutdown(t)
	})

	runA.waitForStartup(t)
	runB.waitForStartup(t)

	master, slave := waitForComplementaryModes(t, nodeA, nodeB)
	if !master.control.hasPeerConnection() {
		t.Fatal("master has no active peer connection")
	}
	if !slave.control.hasPeerConnection() {
		t.Fatal("slave has no active peer connection")
	}
}

func TestMeshCompatMismatchHaltsStartup(t *testing.T) {
	nodeA, nodeB := newMeshPair(t, testCompatSnapshot(t), differentCompatSnapshot(t))

	runA := startIntegrationNode(t, nodeA)
	runB := startIntegrationNode(t, nodeB)
	t.Cleanup(func() {
		runA.shutdown(t)
		runB.shutdown(t)
	})

	waitForMeshCondition(t, "mesh halt due to incompatibility", func() bool {
		haltedA, _ := nodeA.control.haltStatus()
		haltedB, _ := nodeB.control.haltStatus()
		return haltedA || haltedB
	})

	haltedRun := runA
	haltedNode := nodeA
	if halted, _ := nodeA.control.haltStatus(); !halted {
		haltedRun = runB
		haltedNode = nodeB
	}

	res := haltedRun.waitForConnectResult(t, 5*time.Second)
	if res.err == nil {
		t.Fatal("expected Connect error for halted node")
	}
	if !strings.Contains(res.err.Error(), "compatibility hash mismatch") {
		t.Fatalf("Connect error = %q, want compatibility hash mismatch", res.err)
	}

	halted, haltErr := haltedNode.control.haltStatus()
	if !halted {
		t.Fatal("expected node to report halted state")
	}
	if haltErr == nil || !strings.Contains(haltErr.Error(), "compatibility hash mismatch") {
		t.Fatalf("HaltStatus err = %v, want compatibility hash mismatch", haltErr)
	}
}
