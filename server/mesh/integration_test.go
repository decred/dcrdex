package mesh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
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

type memoryEventLogProvider struct {
	mtx      sync.Mutex
	frontier *db.EventLogPosition
	entries  []*db.EventLogEntry
}

func (p *memoryEventLogProvider) EventLogFrontier(context.Context) (*db.EventLogPosition, error) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	return p.frontier, nil
}

func (p *memoryEventLogProvider) EventLogEntriesAfter(_ context.Context, after uint64, limit int) ([]*db.EventLogEntry, error) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	var entries []*db.EventLogEntry
	for _, entry := range p.entries {
		if entry.Seq > after {
			entries = append(entries, entry)
			if len(entries) == limit {
				break
			}
		}
	}
	return entries, nil
}

func (p *memoryEventLogProvider) appendEntry(entry *db.EventLogEntry) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.entries = append(p.entries, entry)
	p.frontier = &db.EventLogPosition{
		Seq:     entry.Seq,
		TipHash: entry.TipHash,
	}
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

func publishEventForTest(ctx context.Context, master *node, entry *eventEnvelope) error {
	state := master.control.currentState()
	if state.activeConn == nil || state.activeConn.link == nil {
		return fmt.Errorf("master has no active connection")
	}
	var ack eventAck
	if err := state.activeConn.link.Request(ctx, eventEnvelopeRoute, &eventBatch{Entries: []*eventEnvelope{entry}}, &ack); err != nil {
		return err
	}
	return nil
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

func TestMeshEventStreamReplaysAndStaysLive(t *testing.T) {
	aListen := reserveTCPAddr(t)
	bListen := reserveTCPAddr(t)
	aPeer := "ws://" + bListen + meshWSPath
	bPeer := "ws://" + aListen + meshWSPath

	entries := []*db.EventLogEntry{
		{Seq: 1, Kind: "stream_test", Event: []byte(`"one"`), TipHash: testTipHash(1)},
		{Seq: 2, Kind: "stream_test", Event: []byte(`"two"`), TipHash: testTipHash(2)},
	}
	aFrontier := &memoryEventLogProvider{
		frontier: &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
		entries:  entries,
	}
	bFrontier := &memoryEventLogProvider{
		frontier: &db.EventLogPosition{},
	}

	entryBySeq := make(map[uint64]*db.EventLogEntry, len(entries))
	for _, entry := range entries {
		entryBySeq[entry.Seq] = entry
	}
	var applied []uint64
	var appliedMtx sync.Mutex
	applyStreamEvent := func(_ context.Context, entry *eventEnvelope) error {
		logEntry := entryBySeq[entry.Seq]
		switch {
		case logEntry != nil:
			bFrontier.appendEntry(logEntry)
		case entry.Seq == 3:
			bFrontier.appendEntry(&db.EventLogEntry{
				Seq:     entry.Seq,
				Kind:    entry.Kind,
				Event:   append([]byte(nil), entry.Payload...),
				TipHash: append([]byte(nil), entry.TipHash...),
			})
		default:
			return fmt.Errorf("unexpected stream seq %d", entry.Seq)
		}
		appliedMtx.Lock()
		applied = append(applied, entry.Seq)
		appliedMtx.Unlock()
		return nil
	}

	nodeA := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:     aListen,
		peerAddr:       aPeer,
		compat:         testCompatSnapshot(t),
		eventLogReader: aFrontier,
	})
	nodeB := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:     bListen,
		peerAddr:       bPeer,
		compat:         testCompatSnapshot(t),
		eventLogReader: bFrontier,
		eventHandler:   applyStreamEvent,
	})
	nodeA.dialer.reconnectInterval = time.Hour
	nodeB.dialer.reconnectInterval = 10 * time.Millisecond

	runA := startIntegrationNode(t, nodeA)
	time.Sleep(100 * time.Millisecond)
	runB := startIntegrationNode(t, nodeB)
	t.Cleanup(func() {
		runA.shutdown(t)
		runB.shutdown(t)
	})

	runA.waitForStartup(t)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if nodeA.control.currentMode() == modeEstablishedMaster && nodeB.control.currentMode() == modeEstablishedSlave {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if nodeA.control.currentMode() != modeEstablishedMaster || nodeB.control.currentMode() != modeEstablishedSlave {
		t.Fatalf("timed out waiting for stream establishment: a=%s b=%s", nodeA.control.currentState(), nodeB.control.currentState())
	}
	runB.waitForStartup(t)
	master, slave := waitForComplementaryModes(t, nodeA, nodeB)
	if master != nodeA || slave != nodeB {
		t.Fatalf("unexpected roles after stream replay: master=%p slave=%p", master, slave)
	}

	appliedMtx.Lock()
	gotApplied := append([]uint64(nil), applied...)
	appliedMtx.Unlock()
	wantApplied := []uint64{1, 2}
	if !reflect.DeepEqual(gotApplied, wantApplied) {
		t.Fatalf("applied stream seqs = %v, want %v", gotApplied, wantApplied)
	}
	frontier, err := bFrontier.EventLogFrontier(context.Background())
	if err != nil {
		t.Fatalf("frontier error: %v", err)
	}
	if frontier.Seq != 2 || !bytes.Equal(frontier.TipHash, testTipHash(2)) {
		t.Fatalf("frontier = %+v, want seq=2 hash=02", frontier)
	}

	aFrontier.appendEntry(&db.EventLogEntry{
		Seq:     3,
		Kind:    "stream_test",
		Event:   []byte(`"three"`),
		TipHash: testTipHash(3),
	})
	master.notifyLocalEventCommitted(3, "", nil)
	waitForMeshCondition(t, "slave event seq 3", func() bool {
		frontier, err := bFrontier.EventLogFrontier(context.Background())
		return err == nil && frontier.Seq == 3
	})
	appliedMtx.Lock()
	gotApplied = append([]uint64(nil), applied...)
	appliedMtx.Unlock()
	wantApplied = []uint64{1, 2, 3}
	if !reflect.DeepEqual(gotApplied, wantApplied) {
		t.Fatalf("applied stream seqs after live tail = %v, want %v", gotApplied, wantApplied)
	}
	frontier, err = bFrontier.EventLogFrontier(context.Background())
	if err != nil {
		t.Fatalf("frontier error after live tail: %v", err)
	}
	if frontier.Seq != 3 || !bytes.Equal(frontier.TipHash, testTipHash(3)) {
		t.Fatalf("frontier after live tail = %+v, want seq=3 hash=03", frontier)
	}
}

func TestMeshEventStreamReconnectResyncsAfterApplyFailure(t *testing.T) {
	aListen := reserveTCPAddr(t)
	bListen := reserveTCPAddr(t)
	aPeer := "ws://" + bListen + meshWSPath
	bPeer := "ws://" + aListen + meshWSPath

	entries := []*db.EventLogEntry{
		{Seq: 1, Kind: "stream_test", Event: []byte(`"one"`), TipHash: testTipHash(1)},
		{Seq: 2, Kind: "stream_test", Event: []byte(`"two"`), TipHash: testTipHash(2)},
	}
	aFrontier := &memoryEventLogProvider{
		frontier: &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
		entries:  entries,
	}
	bFrontier := &memoryEventLogProvider{
		frontier: &db.EventLogPosition{},
	}

	entryBySeq := make(map[uint64]*db.EventLogEntry, len(entries))
	for _, entry := range entries {
		entryBySeq[entry.Seq] = entry
	}

	var (
		applyMtx sync.Mutex
		attempts []uint64
		failed   bool
	)
	firstAttempt := make(chan struct{})
	applyStreamEvent := func(_ context.Context, entry *eventEnvelope) error {
		applyMtx.Lock()
		attempts = append(attempts, entry.Seq)
		if !failed {
			failed = true
			close(firstAttempt)
			applyMtx.Unlock()
			return errors.New("one-shot stream apply failure")
		}
		applyMtx.Unlock()

		logEntry := entryBySeq[entry.Seq]
		if logEntry == nil {
			return fmt.Errorf("unexpected stream seq %d", entry.Seq)
		}
		bFrontier.appendEntry(logEntry)
		return nil
	}

	nodeA := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:     aListen,
		peerAddr:       aPeer,
		compat:         testCompatSnapshot(t),
		eventLogReader: aFrontier,
	})
	nodeB := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:     bListen,
		peerAddr:       bPeer,
		compat:         testCompatSnapshot(t),
		eventLogReader: bFrontier,
		eventHandler:   applyStreamEvent,
	})
	nodeA.dialer.reconnectInterval = time.Hour
	nodeB.dialer.reconnectInterval = 10 * time.Millisecond

	runA := startIntegrationNode(t, nodeA)
	time.Sleep(100 * time.Millisecond)
	runB := startIntegrationNode(t, nodeB)
	t.Cleanup(func() {
		runA.shutdown(t)
		runB.shutdown(t)
	})

	runA.waitForStartup(t)
	select {
	case <-firstAttempt:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for first stream apply attempt")
	}
	frontier, err := bFrontier.EventLogFrontier(context.Background())
	if err != nil {
		t.Fatalf("frontier error after one-shot failure: %v", err)
	}
	if frontier.Seq != 0 {
		t.Fatalf("frontier after one-shot failure = %+v, want seq 0", frontier)
	}

	runB.waitForStartup(t)
	master, slave := waitForComplementaryModes(t, nodeA, nodeB)
	if master != nodeA || slave != nodeB {
		t.Fatalf("unexpected roles after reconnect: master=%p slave=%p", master, slave)
	}

	waitForMeshCondition(t, "slave resynced to seq 2", func() bool {
		frontier, err := bFrontier.EventLogFrontier(context.Background())
		return err == nil && frontier.Seq == 2
	})
	frontier, err = bFrontier.EventLogFrontier(context.Background())
	if err != nil {
		t.Fatalf("frontier error after reconnect: %v", err)
	}
	if frontier.Seq != 2 || !bytes.Equal(frontier.TipHash, testTipHash(2)) {
		t.Fatalf("frontier after reconnect = %+v, want seq=2 hash=02", frontier)
	}

	applyMtx.Lock()
	gotAttempts := append([]uint64(nil), attempts...)
	applyMtx.Unlock()
	if len(gotAttempts) < 3 || gotAttempts[0] != 1 || gotAttempts[1] != 1 {
		t.Fatalf("stream attempts = %v, want first failure and replay of seq 1", gotAttempts)
	}
}

// Mid-batch apply failure: seq 1 is durable, reconnect resumes at seq 2.
func TestMeshEventStreamReconnectResyncsAfterMidBatchApplyFailure(t *testing.T) {
	entries := []*db.EventLogEntry{
		{Seq: 1, Kind: "stream_test", Event: []byte(`"one"`), TipHash: testTipHash(1)},
		{Seq: 2, Kind: "stream_test", Event: []byte(`"two"`), TipHash: testTipHash(2)},
	}
	aFrontier := &memoryEventLogProvider{
		frontier: &db.EventLogPosition{Seq: 2, TipHash: testTipHash(2)},
		entries:  entries,
	}
	bFrontier := &memoryEventLogProvider{frontier: &db.EventLogPosition{}}

	var (
		applyMtx sync.Mutex
		attempts []uint64
		failOnce = true
	)
	failed := make(chan struct{})
	apply := func(_ context.Context, env *eventEnvelope) error {
		applyMtx.Lock()
		attempts = append(attempts, env.Seq)
		if env.Seq == 2 && failOnce {
			failOnce = false
			applyMtx.Unlock()
			close(failed)
			return errors.New("mid-batch apply failure")
		}
		applyMtx.Unlock()
		for _, e := range entries {
			if e.Seq == env.Seq {
				bFrontier.appendEntry(e)
				return nil
			}
		}
		return fmt.Errorf("unexpected seq %d", env.Seq)
	}

	aListen, bListen := reserveTCPAddr(t), reserveTCPAddr(t)
	nodeA := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:     aListen,
		peerAddr:       "ws://" + bListen + meshWSPath,
		compat:         testCompatSnapshot(t),
		eventLogReader: aFrontier,
	})
	nodeB := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:     bListen,
		peerAddr:       "ws://" + aListen + meshWSPath,
		compat:         testCompatSnapshot(t),
		eventLogReader: bFrontier,
		eventHandler:   apply,
	})
	nodeA.dialer.reconnectInterval = time.Hour
	nodeB.dialer.reconnectInterval = 10 * time.Millisecond

	runA := startIntegrationNode(t, nodeA)
	time.Sleep(100 * time.Millisecond)
	runB := startIntegrationNode(t, nodeB)
	t.Cleanup(func() { runA.shutdown(t); runB.shutdown(t) })

	runA.waitForStartup(t)
	select {
	case <-failed:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for mid-batch failure")
	}
	if f, _ := bFrontier.EventLogFrontier(context.Background()); f.Seq != 1 {
		t.Fatalf("frontier after partial apply = %+v, want seq 1", f)
	}

	runB.waitForStartup(t)
	if master, slave := waitForComplementaryModes(t, nodeA, nodeB); master != nodeA || slave != nodeB {
		t.Fatalf("unexpected roles: master=%p slave=%p", master, slave)
	}
	waitForMeshCondition(t, "slave at seq 2", func() bool {
		f, err := bFrontier.EventLogFrontier(context.Background())
		return err == nil && f.Seq == 2
	})

	applyMtx.Lock()
	got := append([]uint64(nil), attempts...)
	applyMtx.Unlock()
	// apply 1, fail 2, reconnect from frontier 1, apply only 2.
	if want := []uint64{1, 2, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("attempts = %v, want %v", got, want)
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

func TestMeshRequestForwardsLimitToMaster(t *testing.T) {
	var (
		handlerMtx   sync.Mutex
		calls        int
		gotCommandID string
		gotUser      account.AccountID
		gotMsgID     uint64
		gotRoute     string
	)
	handler := func(ctx context.Context, commandID string, req CommandRequest) *msgjson.Error {
		handlerMtx.Lock()
		calls++
		gotCommandID = commandID
		gotUser = req.User
		gotMsgID = req.Msg.ID
		gotRoute = req.Msg.Route
		handlerMtx.Unlock()
		return nil
	}
	nodeA, nodeB := func() (*node, *node) {
		aListen := reserveTCPAddr(t)
		bListen := reserveTCPAddr(t)
		aPeer := "ws://" + bListen + meshWSPath
		bPeer := "ws://" + aListen + meshWSPath
		return newIntegrationNode(t, integrationNodeConfig{
				listenAddr: aListen,
				peerAddr:   aPeer,
				compat:     testCompatSnapshot(t),
				cmdHandler: handler,
			}),
			newIntegrationNode(t, integrationNodeConfig{
				listenAddr: bListen,
				peerAddr:   bPeer,
				compat:     testCompatSnapshot(t),
				cmdHandler: handler,
			})
	}()

	runA := startIntegrationNode(t, nodeA)
	runB := startIntegrationNode(t, nodeB)
	t.Cleanup(func() {
		runA.shutdown(t)
		runB.shutdown(t)
	})

	runA.waitForStartup(t)
	runB.waitForStartup(t)

	master, slave := waitForComplementaryModes(t, nodeA, nodeB)
	masterConn, ok := master.control.currentState().activeConn.link.(*rpcConn)
	if !ok {
		t.Fatalf("master active conn type = %T", master.control.currentState().activeConn.link)
	}
	slaveConn, ok := slave.control.currentState().activeConn.link.(*rpcConn)
	if !ok {
		t.Fatalf("slave active conn type = %T", slave.control.currentState().activeConn.link)
	}
	if !masterConn.Authed() || !slaveConn.Authed() {
		t.Fatalf("authed flags before request: master=%v slave=%v", masterConn.Authed(), slaveConn.Authed())
	}

	var user account.AccountID
	user[0] = 0x99

	req, err := msgjson.NewRequest(77, msgjson.LimitRoute, map[string]string{"market": "dcr_btc"})
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}

	const commandID = "prepared-limit-command"
	rpcErr, outcomeUnknown := slave.forwardCommand(context.Background(), &commandForward{
		CommandID: commandID,
		Kind:      "limit",
		User:      user,
		Msg:       req,
	})
	if rpcErr != nil {
		t.Fatalf("slave forwardCommand error: %v", rpcErr)
	}
	if outcomeUnknown {
		t.Fatal("acked forward reported an unknown outcome")
	}

	handlerMtx.Lock()
	defer handlerMtx.Unlock()
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
	if gotCommandID != commandID {
		t.Fatalf("handler command id = %q, want %q", gotCommandID, commandID)
	}
	if gotUser != user {
		t.Fatalf("handler user = %v, want %v", gotUser, user)
	}
	if gotMsgID != req.ID {
		t.Fatalf("handler msg id = %d, want %d", gotMsgID, req.ID)
	}
	if gotRoute != msgjson.LimitRoute {
		t.Fatalf("handler route = %q, want %q", gotRoute, msgjson.LimitRoute)
	}
}

func TestMeshSendCommandResult(t *testing.T) {
	type recordedResult struct {
		commandID string
		result    json.RawMessage
	}
	type resultRecorder struct {
		mtx     sync.Mutex
		results []recordedResult
	}
	record := func(rec *resultRecorder) func(string, json.RawMessage) {
		return func(commandID string, result json.RawMessage) {
			rec.mtx.Lock()
			defer rec.mtx.Unlock()
			rec.results = append(rec.results, recordedResult{
				commandID: commandID,
				result:    append([]byte(nil), result...),
			})
		}
	}

	recA := new(resultRecorder)
	recB := new(resultRecorder)

	aListen := reserveTCPAddr(t)
	bListen := reserveTCPAddr(t)
	aPeer := "ws://" + bListen + meshWSPath
	bPeer := "ws://" + aListen + meshWSPath

	nodeA := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:    aListen,
		peerAddr:      aPeer,
		compat:        testCompatSnapshot(t),
		resultHandler: record(recA),
	})
	nodeB := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:    bListen,
		peerAddr:      bPeer,
		compat:        testCompatSnapshot(t),
		resultHandler: record(recB),
	})

	runA := startIntegrationNode(t, nodeA)
	runB := startIntegrationNode(t, nodeB)
	t.Cleanup(func() {
		runA.shutdown(t)
		runB.shutdown(t)
	})

	runA.waitForStartup(t)
	runB.waitForStartup(t)

	master, slave := waitForComplementaryModes(t, nodeA, nodeB)
	masterConn, ok := master.control.currentState().activeConn.link.(*rpcConn)
	if !ok {
		t.Fatalf("master active conn type = %T", master.control.currentState().activeConn.link)
	}
	slaveConn, ok := slave.control.currentState().activeConn.link.(*rpcConn)
	if !ok {
		t.Fatalf("slave active conn type = %T", slave.control.currentState().activeConn.link)
	}
	waitForMeshCondition(t, "authorized mesh links", func() bool {
		return masterConn.Authed() && slaveConn.Authed()
	})

	result := map[string]string{"status": "ok"}
	if err := master.sendCommandResult(context.Background(), &commandResult{
		CommandID: "cmd-ok",
		Result:    mustMarshalJSON(t, result),
	}); err != nil {
		t.Fatalf("sendCommandResult error: %v", err)
	}

	var results []recordedResult
	if slave == nodeA {
		recA.mtx.Lock()
		results = append([]recordedResult(nil), recA.results...)
		recA.mtx.Unlock()
	} else {
		recB.mtx.Lock()
		results = append([]recordedResult(nil), recB.results...)
		recB.mtx.Unlock()
	}
	if len(results) != 1 {
		t.Fatalf("received command results = %d, want 1", len(results))
	}
	if results[0].commandID != "cmd-ok" {
		t.Fatalf("received command id = %q, want cmd-ok", results[0].commandID)
	}
	requireJSONResult(t, results[0].result, result)
}

func TestMeshPublishEvent(t *testing.T) {
	recA := new(meshEventRecorder)
	recB := new(meshEventRecorder)
	frontierA := &memoryEventLogProvider{frontier: &db.EventLogPosition{}}
	frontierB := &memoryEventLogProvider{frontier: &db.EventLogPosition{}}

	aListen := reserveTCPAddr(t)
	bListen := reserveTCPAddr(t)
	aPeer := "ws://" + bListen + meshWSPath
	bPeer := "ws://" + aListen + meshWSPath

	nodeA := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:     aListen,
		peerAddr:       aPeer,
		compat:         testCompatSnapshot(t),
		eventLogReader: frontierA,
		eventHandler:   recA.handler(frontierA),
	})
	nodeB := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:     bListen,
		peerAddr:       bPeer,
		compat:         testCompatSnapshot(t),
		eventLogReader: frontierB,
		eventHandler:   recB.handler(frontierB),
	})

	runA := startIntegrationNode(t, nodeA)
	t.Cleanup(func() {
		runA.shutdown(t)
	})
	runA.assertStartupPending(t, 200*time.Millisecond)

	runB := startIntegrationNode(t, nodeB)
	t.Cleanup(func() {
		runB.shutdown(t)
	})
	runB.waitForStartup(t)
	runA.waitForStartup(t)

	master, slave := waitForComplementaryModes(t, nodeA, nodeB)
	masterConn, ok := master.control.currentState().activeConn.link.(*rpcConn)
	if !ok {
		t.Fatalf("master active conn type = %T", master.control.currentState().activeConn.link)
	}
	slaveConn, ok := slave.control.currentState().activeConn.link.(*rpcConn)
	if !ok {
		t.Fatalf("slave active conn type = %T", slave.control.currentState().activeConn.link)
	}
	waitForMeshCondition(t, "authorized mesh links", func() bool {
		return masterConn.Authed() && slaveConn.Authed()
	})

	if err := publishEventForTest(context.Background(), master, &eventEnvelope{
		Seq:             1,
		TipHash:         testTipHash(1),
		MasterTip:       1,
		Kind:            "test_order_accepted",
		OriginCommandID: "cmd-88",
		Payload:         []byte(`{"oid":"abc123"}`),
	}); err != nil {
		t.Fatalf("PublishEvent #1 error: %v", err)
	}
	if err := publishEventForTest(context.Background(), master, &eventEnvelope{
		Seq:       2,
		TipHash:   testTipHash(2),
		MasterTip: 2,
		Kind:      "epoch_ended",
		Payload:   []byte(`{"epoch":7}`),
	}); err != nil {
		t.Fatalf("PublishEvent #2 error: %v", err)
	}

	waitForMeshCondition(t, "slave event seq 2", func() bool {
		if slave == nodeA {
			return recA.count() == 2
		}
		return recB.count() == 2
	})

	var applied []eventEnvelope
	if slave == nodeA {
		applied = recA.events()
	} else {
		applied = recB.events()
	}

	if len(applied) != 2 {
		t.Fatalf("applied events = %d, want 2", len(applied))
	}
	if applied[0].Seq != 1 || applied[0].Kind != "test_order_accepted" {
		t.Fatalf("first event = %+v, want seq=1 kind=order_accepted", applied[0])
	}
	if applied[0].OriginCommandID != "cmd-88" {
		t.Fatalf("first event origin command id = %q, want cmd-88", applied[0].OriginCommandID)
	}
	if string(applied[0].Payload) != `{"oid":"abc123"}` {
		t.Fatalf("first event payload = %s", applied[0].Payload)
	}
	if applied[1].Seq != 2 || applied[1].Kind != "epoch_ended" {
		t.Fatalf("second event = %+v, want seq=2 kind=epoch_ended", applied[1])
	}
	if string(applied[1].Payload) != `{"epoch":7}` {
		t.Fatalf("second event payload = %s", applied[1].Payload)
	}
}

type meshEventRecorder struct {
	mtx       sync.Mutex
	envelopes []eventEnvelope
}

func (r *meshEventRecorder) handler(frontier *memoryEventLogProvider) func(context.Context, *eventEnvelope) error {
	return func(_ context.Context, entry *eventEnvelope) error {
		r.mtx.Lock()
		defer r.mtx.Unlock()
		copied := *entry
		copied.TipHash = append([]byte(nil), entry.TipHash...)
		copied.Payload = append([]byte(nil), entry.Payload...)
		copied.CommandResult = append([]byte(nil), entry.CommandResult...)
		r.envelopes = append(r.envelopes, copied)
		frontier.appendEntry(&db.EventLogEntry{
			Seq:     entry.Seq,
			Kind:    entry.Kind,
			Event:   append([]byte(nil), entry.Payload...),
			TipHash: append([]byte(nil), entry.TipHash...),
		})
		return nil
	}
}

func (r *meshEventRecorder) count() int {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	return len(r.envelopes)
}

func (r *meshEventRecorder) events() []eventEnvelope {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	return append([]eventEnvelope(nil), r.envelopes...)
}

func TestMeshPreparingMasterDefersStartupEventStreamUntilReady(t *testing.T) {
	recA := new(meshEventRecorder)
	recB := new(meshEventRecorder)
	frontierA := &memoryEventLogProvider{frontier: &db.EventLogPosition{}}
	frontierB := &memoryEventLogProvider{frontier: &db.EventLogPosition{}}

	aListen := reserveTCPAddr(t)
	bListen := reserveTCPAddr(t)
	aPeer := "ws://" + bListen + meshWSPath
	bPeer := "ws://" + aListen + meshWSPath

	nodeA := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:     aListen,
		peerAddr:       aPeer,
		compat:         testCompatSnapshot(t),
		eventLogReader: frontierA,
		eventHandler:   recA.handler(frontierA),
		lifecycle:      &lifecycleHooks{},
	})
	nodeB := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:     bListen,
		peerAddr:       bPeer,
		compat:         testCompatSnapshot(t),
		eventLogReader: frontierB,
		eventHandler:   recB.handler(frontierB),
		lifecycle:      &lifecycleHooks{},
	})

	runA := startIntegrationNode(t, nodeA)
	runB := startIntegrationNode(t, nodeB)
	t.Cleanup(func() {
		runA.shutdown(t)
		runB.shutdown(t)
	})

	waitForMeshCondition(t, "preparing master and established slave", func() bool {
		aState := nodeA.control.currentState()
		bState := nodeB.control.currentState()
		paired := aState.activeConn != nil && bState.activeConn != nil &&
			aState.activeConn.initiatorNodeID == bState.activeConn.initiatorNodeID
		return paired && ((aState.mode == modePreparingMaster && bState.mode == modeEstablishedSlave) ||
			(aState.mode == modeEstablishedSlave && bState.mode == modePreparingMaster))
	})

	master, slaveRec, masterFrontier := nodeA, recB, frontierA
	if nodeB.control.currentMode() == modePreparingMaster {
		master, slaveRec, masterFrontier = nodeB, recA, frontierB
	}

	if err := master.checkEventPublishAvailable(false); err != nil {
		t.Fatalf("direct event unavailable during master preparation: %v", err)
	}
	if err := master.checkEventPublishAvailable(true); err == nil {
		t.Fatalf("forwarded-command event available during master preparation")
	}

	startupEntry := &db.EventLogEntry{
		Seq:     1,
		Kind:    "test_market_started",
		Event:   []byte(`{"market":"dcr_btc"}`),
		TipHash: testTipHash(1),
	}
	masterFrontier.appendEntry(startupEntry)
	master.notifyLocalEventCommitted(startupEntry.Seq, "", nil)

	time.Sleep(100 * time.Millisecond)
	if got := slaveRec.count(); got != 0 {
		t.Fatalf("slave received %d events before master readiness, want 0", got)
	}

	if err := master.notifyMasterReady(); err != nil {
		t.Fatalf("notifyMasterReady error: %v", err)
	}
	runA.waitForStartup(t)
	runB.waitForStartup(t)
	waitForComplementaryModes(t, nodeA, nodeB)

	waitForMeshCondition(t, "slave received market_started event", func() bool {
		return slaveRec.count() == 1
	})
	got := slaveRec.events()[0]
	if got.Seq != startupEntry.Seq || got.MasterTip != startupEntry.Seq || got.Kind != startupEntry.Kind {
		t.Fatalf("event envelope = %+v, want seq/tip=%d kind=%q", got, startupEntry.Seq, startupEntry.Kind)
	}
	if got.OriginCommandID != "" || len(got.CommandResult) != 0 {
		t.Fatalf("event command ID/result = %q/%x, want empty", got.OriginCommandID, got.CommandResult)
	}
	if string(got.Payload) != string(startupEntry.Event) {
		t.Fatalf("event payload = %s, want %s", got.Payload, startupEntry.Event)
	}
}

func TestMeshProxyClientMessage(t *testing.T) {
	type proxiedCall struct {
		user      account.AccountID
		msgType   msgjson.MessageType
		route     string
		msgID     uint64
		timeout   time.Duration
		broadcast bool
	}

	var (
		callMtx sync.Mutex
		calls   []proxiedCall
	)

	proxiedHandler := func(_ context.Context, req *ClientProxyMessage) error {
		time.Sleep(20 * time.Millisecond)
		callMtx.Lock()
		calls = append(calls, proxiedCall{
			user:      req.User,
			msgType:   req.Msg.Type,
			route:     req.Msg.Route,
			msgID:     req.Msg.ID,
			timeout:   time.Duration(req.TimeoutMS) * time.Millisecond,
			broadcast: req.Broadcast,
		})
		callMtx.Unlock()
		return nil
	}

	aListen := reserveTCPAddr(t)
	bListen := reserveTCPAddr(t)
	aPeer := "ws://" + bListen + meshWSPath
	bPeer := "ws://" + aListen + meshWSPath

	nodeA := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:   aListen,
		peerAddr:     aPeer,
		compat:       testCompatSnapshot(t),
		proxyHandler: proxiedHandler,
	})
	nodeB := newIntegrationNode(t, integrationNodeConfig{
		listenAddr:   bListen,
		peerAddr:     bPeer,
		compat:       testCompatSnapshot(t),
		proxyHandler: proxiedHandler,
	})

	runA := startIntegrationNode(t, nodeA)
	runB := startIntegrationNode(t, nodeB)
	t.Cleanup(func() {
		runA.shutdown(t)
		runB.shutdown(t)
	})

	runA.waitForStartup(t)
	runB.waitForStartup(t)

	master, slave := waitForComplementaryModes(t, nodeA, nodeB)

	var user account.AccountID
	user[0] = 0x44

	req, err := msgjson.NewRequest(88, msgjson.PreimageRoute, map[string]string{"mkt": "dcr_btc"})
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}

	err = master.sendClientProxyMessage(context.Background(), &ClientProxyMessage{
		User:      user,
		Msg:       req,
		TimeoutMS: 50,
	})
	if err != nil {
		t.Fatalf("ProxyClientMessage error: %v", err)
	}

	resp, err := msgjson.NewResponse(89, struct{}{}, nil)
	if err != nil {
		t.Fatalf("NewResponse error: %v", err)
	}
	err = slave.sendClientProxyMessage(context.Background(), &ClientProxyMessage{
		User: user,
		Msg:  resp,
	})
	if err != nil {
		t.Fatalf("response ProxyClientMessage error: %v", err)
	}

	note, err := msgjson.NewNotification(msgjson.NotifyRoute, "scheduled maintenance in 10 minutes")
	if err != nil {
		t.Fatalf("NewNotification error: %v", err)
	}
	err = master.sendClientProxyMessage(context.Background(), &ClientProxyMessage{
		Msg:       note,
		Broadcast: true,
	})
	if err != nil {
		t.Fatalf("broadcast ProxyClientMessage error: %v", err)
	}

	callMtx.Lock()
	defer callMtx.Unlock()
	if len(calls) != 3 {
		t.Fatalf("proxied handler calls = %d, want 3", len(calls))
	}
	if calls[0].user != user {
		t.Fatalf("proxied handler user = %v, want %v", calls[0].user, user)
	}
	if calls[0].msgType != msgjson.Request {
		t.Fatalf("proxied handler message type = %v, want request", calls[0].msgType)
	}
	if calls[0].route != msgjson.PreimageRoute {
		t.Fatalf("proxied handler route = %q, want %q", calls[0].route, msgjson.PreimageRoute)
	}
	if calls[0].msgID != req.ID {
		t.Fatalf("proxied handler msg id = %d, want %d", calls[0].msgID, req.ID)
	}
	if calls[0].timeout != 50*time.Millisecond {
		t.Fatalf("proxied handler timeout = %v, want %v", calls[0].timeout, 50*time.Millisecond)
	}
	if calls[1].user != user {
		t.Fatalf("response handler user = %v, want %v", calls[1].user, user)
	}
	if calls[1].msgType != msgjson.Response {
		t.Fatalf("response handler message type = %v, want response", calls[1].msgType)
	}
	if calls[1].msgID != resp.ID {
		t.Fatalf("response handler msg id = %d, want %d", calls[1].msgID, resp.ID)
	}
	if calls[0].broadcast || calls[1].broadcast {
		t.Fatalf("per-user proxied messages arrived flagged as broadcast")
	}
	if !calls[2].broadcast {
		t.Fatalf("broadcast flag not relayed")
	}
	if calls[2].msgType != msgjson.Notification {
		t.Fatalf("broadcast handler message type = %v, want notification", calls[2].msgType)
	}
	if calls[2].route != msgjson.NotifyRoute {
		t.Fatalf("broadcast handler route = %q, want %q", calls[2].route, msgjson.NotifyRoute)
	}
}
