// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// meshApplication lets the node deliver peer commands, events, and results to
// Service, and relay client messages and connection queries.
type meshApplication interface {
	executeForwardedCommand(context.Context, string, CommandRequest) *msgjson.Error
	receiveCommandFailure(string, *msgjson.Error)
	receiveCommandResult(string, json.RawMessage)
	applyReceivedEvent(context.Context, *eventEnvelope) error
	handleClientProxyMessage(context.Context, *ClientProxyMessage) error
	answerClientConnected([]account.AccountID) []account.AccountID
}

// lifecycleHooks are lifecycle notifications sent from the node to its creator.
type lifecycleHooks struct {
	becameMaster              func()
	peerClientEndpointChanged func(string, []byte)
	failPendingCommands       func(reason string)
	halted                    func(error)
}

// nodeConfig is configuration of a mesh node.
type nodeConfig struct {
	app       meshApplication
	lifecycle lifecycleHooks

	dataDir    string
	listenAddr string
	rpcKey     string
	rpcCert    string
	noTLS      bool
	compat     *CompatSnapshot
	// dexPrivKey is the server signing identity already used for client-facing
	// messages. In the current mesh design, both nodes share that key so
	// client-facing signatures do not change during failover, and mesh hello
	// payloads are authenticated with the same shared identity.
	dexPrivKey     *secp256k1.PrivateKey
	eventLogReader db.EventLogReader
	snapshotStore  db.SnapshotStore
	logger         dex.Logger

	peerAddr string
	peerCert []byte

	// clientHost and clientCert are advertised in the signed mesh hello.
	clientHost string
	clientCert []byte

	// initialFrontier is the event-log frontier at service construction time.
	// It may be nil for a brand-new node.
	initialFrontier *db.EventLogPosition
}

func (cfg *nodeConfig) validate() error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	if cfg.dataDir == "" {
		return fmt.Errorf("empty data dir")
	}
	if cfg.listenAddr == "" {
		return fmt.Errorf("empty listen address")
	}
	if cfg.peerAddr == "" {
		return fmt.Errorf("empty peer address")
	}
	if !cfg.noTLS && (cfg.rpcKey == "" || cfg.rpcCert == "") {
		return fmt.Errorf("missing cert pair file")
	}
	if cfg.compat == nil {
		return fmt.Errorf("nil compatibility snapshot")
	}
	if cfg.dexPrivKey == nil {
		return fmt.Errorf("nil DEX private key")
	}
	if cfg.eventLogReader == nil {
		return fmt.Errorf("nil event log reader")
	}
	if cfg.snapshotStore == nil {
		return fmt.Errorf("nil snapshot store")
	}
	if cfg.app == nil {
		return fmt.Errorf("nil mesh application")
	}
	return nil
}

// node is the peered implementation of meshTransport (as opposed to
// singleServerTransport). It manages the communication with the peer,
// the lifecycle of the node, and calls back into the meshApplication as
// needed. It implements the meshTransport interface.
type node struct {
	log          dex.Logger
	nodeID       string
	handshakeSvc *handshakeService
	handshakes   *handshakeSessions
	dialer       *outboundDialer
	serverCfg    meshServerConfig
	app          meshApplication

	control       *controlLoop
	stream        *eventStreamManager
	snapshots     *snapshotServer
	lifecycle     lifecycleHooks
	applyFailures applyFailureStreak

	eventLogReader db.EventLogReader
	snapshotStore  db.SnapshotStore

	// eventsGate is used to block sending events to the application until
	// it is ready to receive them.
	eventsGate *readiness

	// seeding marks a first join in progress: set at construction for an
	// empty-log node, cleared at seed-orchestrator teardown.
	seeding atomic.Bool

	routeTable map[string]meshRoute
	cancelRun  context.CancelFunc
	runContext context.Context
}

var _ meshTransport = (*node)(nil)

// newNode constructs the mesh node.
func newNode(cfg *nodeConfig) (*node, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	var initialEventSeq uint64
	if cfg.initialFrontier != nil {
		initialEventSeq = cfg.initialFrontier.Seq
	}

	logger := cfg.logger
	if logger == nil {
		logger = dex.Disabled
	}

	nodeID, err := loadOrCreateNodeID(cfg.dataDir)
	if err != nil {
		return nil, fmt.Errorf("loadOrCreateNodeID: %w", err)
	}

	peerParsed, err := parsePeerURL(cfg.peerAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid peer address: %w", err)
	}
	peerURL := peerParsed.String()

	node := &node{
		log:    logger,
		nodeID: nodeID,
		serverCfg: meshServerConfig{
			ListenAddr: cfg.listenAddr,
			RPCKey:     cfg.rpcKey,
			RPCCert:    cfg.rpcCert,
			NoTLS:      cfg.noTLS,
		},
		app:            cfg.app,
		lifecycle:      cfg.lifecycle,
		eventLogReader: cfg.eventLogReader,
		snapshotStore:  cfg.snapshotStore,
		eventsGate:     newReadiness(),
	}
	if initialEventSeq == 0 {
		node.seeding.Store(true)
	}
	node.control = newControlLoop(logger, nodeID, node)
	node.stream = newEventStreamManager(&eventStreamManagerConfig{
		log:                logger,
		eventLogReader:     cfg.eventLogReader,
		node:               node,
		initialFrontierSeq: initialEventSeq,
	})
	node.snapshots = newSnapshotServer(logger, cfg.snapshotStore, node)

	signer, err := newSecp256k1Signer(cfg.dexPrivKey)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize signer: %w", err)
	}

	node.handshakeSvc = newHandshakeService(
		nodeID,
		node.control.currentRole,
		signer,
		cfg.compat,
		cfg.eventLogReader,
		cfg.clientHost,
		cfg.clientCert,
		logger,
	)
	node.handshakes = newHandshakeSessions(logger, node.handshakeSvc, node)

	dialer, err := newOutboundDialer(
		peerURL,
		cfg.peerCert,
		logger,
		node.handshakeSvc,
		node.routes(),
		node,
	)
	if err != nil {
		return nil, err
	}
	node.dialer = dialer

	return node, nil
}

// connect starts the node. It returns once startup has resolved: the node is
// established as master, established as slave, or has halted due to
// incompatibility with the configured peer.
func (n *node) connect(ctx context.Context) (*sync.WaitGroup, error) {
	n.log.Infof("Mesh starting. Local node ID %s", n.nodeID)

	server, err := newMeshServer(&n.serverCfg, n.routes(), n.log)
	if err != nil {
		return nil, err
	}

	wg := new(sync.WaitGroup)

	runCtx, cancel := context.WithCancel(ctx)
	startup := n.startLoops(runCtx, cancel, wg)

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := server.Run(runCtx); err != nil {
			n.log.Errorf("mesh server error: %v", err)
			cancel()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		n.dialer.Run(runCtx)
	}()

	if err := n.waitForStartup(runCtx, startup); err != nil {
		cancel()
		wg.Wait()
		return nil, err
	}

	return wg, nil
}

// notifyReadyForEvents notifies that the application is ready to receive
// and apply streamed events.
func (n *node) notifyReadyForEvents() {
	n.eventsGate.resolve(nil)
}

// eventsGateOpen reports whether notifyReadyForEvents has been called.
func (n *node) eventsGateOpen() bool {
	if n.eventsGate == nil {
		return true
	}
	select {
	case <-n.eventsGate.resolved():
		return true
	default:
		return false
	}
}

// applyWedgeStreakThreshold is consecutive apply failures at one seq before
// the node treats the failure as deterministic and halts.
const applyWedgeStreakThreshold = 3

// applyFailureStreak counts consecutive apply failures at one seq.
type applyFailureStreak struct {
	mtx   sync.Mutex
	seq   uint64
	count int
}

// observe returns the consecutive failure count for seq (0 on success). A
// different seq restarts the count. A dead apply context reports 0 without
// resetting the streak.
func (e *applyFailureStreak) observe(ctx context.Context, seq uint64, err error) int {
	e.mtx.Lock()
	defer e.mtx.Unlock()
	if err == nil {
		e.seq, e.count = 0, 0
		return 0
	}
	if ctx.Err() != nil {
		return 0
	}
	if seq != e.seq {
		e.seq, e.count = seq, 1
	} else {
		e.count++
	}
	return e.count
}

// replicationWedgedError is returned when the slave has tried to apply a replicated
// event multiple times, and failed.
type replicationWedgedError struct {
	Seq      uint64
	Attempts int
	Err      error
}

func (err *replicationWedgedError) Error() string {
	return fmt.Sprintf("replicated event at seq %d failed to apply %d consecutive times: %v.",
		err.Seq, err.Attempts, err.Err)
}

func (err *replicationWedgedError) Unwrap() error {
	return err.Err
}

// applyInboundEventEnvelope is used by the slave to apply an event received from the master.
func (n *node) applyInboundEventEnvelope(peerConn *nodeConn, entry *eventEnvelope) error {
	ctx := n.runContext
	if err := n.app.applyReceivedEvent(ctx, entry); err != nil {
		if isTerminalEventApplyFailure(err) {
			n.postTerminalApplyFailureIfNeeded(err)
			return err
		}
		if count := n.applyFailures.observe(ctx, entry.Seq, err); count >= applyWedgeStreakThreshold {
			n.postTerminalApplyFailureIfNeeded(&replicationWedgedError{
				Seq: entry.Seq, Attempts: count, Err: err,
			})
		}
		return err
	}

	n.applyFailures.observe(ctx, entry.Seq, nil)
	n.stream.eventCommitted(entry.Seq, "", nil)
	n.postStreamCaughtUpIfAtMasterTip(peerConn, entry)

	return nil
}

// startLoops starts the control loop and the event stream manager.
func (n *node) startLoops(ctx context.Context, cancel context.CancelFunc, wg *sync.WaitGroup) *readiness {
	startup := newReadiness()
	n.cancelRun = cancel
	n.runContext = ctx
	n.control.prepareRun(ctx)

	wg.Add(1)
	go func() {
		defer wg.Done()
		n.control.run(ctx, startup)
	}()

	streamReady := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := n.stream.run(ctx, streamReady); err != nil && ctx.Err() == nil {
			n.log.Errorf("eventStreamManager.run error: %v", err)
			cancel()
		}
	}()
	<-streamReady

	return startup
}

// waitForStartup blocks until startup resolves or the run context ends. A
// context end is refined into the halt error when the control loop halted.
func (n *node) waitForStartup(runCtx context.Context, startup *readiness) error {
	if err := startup.wait(runCtx); err != nil {
		state := n.control.currentState()
		if state.mode == modeHalted {
			if state.haltErr != nil {
				return state.haltErr
			}
			return fmt.Errorf("mesh halted during startup")
		}
		return fmt.Errorf("mesh stopped during startup")
	}
	return nil
}

// controlHalted is a callback passed to the control loop to handle
// the situation where the node has been halted.
func (n *node) controlHalted(err error) {
	n.cancelRun()
	n.lifecycle.halted(err)
}

// haltStatus reports the control loop's terminal halt state.
func (n *node) haltStatus() (bool, error) {
	return n.control.haltStatus()
}

// handleEffect handles an effect from the control loop.
func (n *node) handleEffect(eff effect) {
	switch e := eff.(type) {
	case effectBecameMaster:
		n.lifecycle.becameMaster()
	case effectDisconnect:
		n.stopStreamForConn(e.Conn)
		e.Conn.link.Disconnect()
	case effectStopEventStream:
		n.stopStreamForConn(e.Conn)
	case effectWatchConn:
		n.watchPeerConn(e.Conn)
	case effectStartSubscriber:
		go n.runSubscriber(e.Conn)
	case effectStartEventStream:
		n.startEventStream(e.Conn, e.SlaveFrontier)
	case effectStartSnapshotSend:
		n.startSnapshotSend(e.Conn)
	case effectPeerClientEndpointChanged:
		n.lifecycle.peerClientEndpointChanged(e.Host, append([]byte(nil), e.Cert...))
	case effectFailPendingCommands:
		n.lifecycle.failPendingCommands(e.Reason)
	}
}

// watchPeerConn starts a goroutine to monitor a peer connection and report a
// disconnection event to the control loop when the connection is closed.
// If this node is the master, it also starts a timer to make sure the slave
// establishes a stream in time.
func (n *node) watchPeerConn(peerConn *nodeConn) {
	go func() {
		<-peerConn.link.Done()
		_ = n.control.post(connectionDisconnectedSignal{conn: peerConn, at: time.Now()})
	}()
	go n.watchSubscribeTimeout(peerConn)
}

// errConnNotAdopted means this conn lost adoption to an existing session
// with the same peer.
var errConnNotAdopted = errors.New("connection not adopted")

// applyHandshakeResult hands a completed wire handshake to the control loop,
// which decides whether to adopt the connection and which mode this node
// moves to (possibly halting it).
//   - nil means the connection was adopted as the active connection
//   - Any error means the connection was not adopted and the caller must disconnect it.
//   - errConnNotAdopted specifically means the handshake itself was fine but an
//     existing session with this peer took precedence.
func (n *node) applyHandshakeResult(ctx context.Context, conn link, result *handshakeResult, initiatorNodeID string) error {
	peerConn := newNodeConn(conn, result.peerHello.NodeID, initiatorNodeID)
	peerRole := result.peerHello.Role
	progress := result.progress
	peerFrontier := fromFrontierMessage(result.peerHello.Frontier)

	localFrontier, err := n.eventLogReader.EventLogFrontier(ctx)
	if err != nil {
		return fmt.Errorf("local event log frontier: %w", err)
	}

	res, err := n.control.send(handshakeResolvedSignal{
		conn:          peerConn,
		peerRole:      peerRole,
		progress:      progress,
		localFrontier: localFrontier,
		peerFrontier:  peerFrontier,
		clientHost:    result.clientHost,
		clientCert:    append([]byte(nil), result.clientCert...),
		at:            time.Now(),
	})
	if err != nil {
		return err
	}
	if res.err != nil {
		return res.err
	}

	state := res.state

	switch outcome := res.outcome.(type) {
	case handshakeOutcome:
		switch outcome {
		case handshakeAdopted:
			n.log.Infof("Mesh handshake with peer %s resolved: peer_role=%s progress=%s local_state=%s",
				meshPeerLogID(peerConn), peerRole, progress, state.mode)
			return nil
		case handshakeNotAdopted:
			n.log.Infof("Mesh handshake with peer %s resolved: peer_role=%s progress=%s connection_not_adopted local_state=%s",
				meshPeerLogID(peerConn), peerRole, progress, state.mode)
			return errConnNotAdopted
		case handshakeDivergedJoinRejected:
			n.log.Warnf("Mesh peer %s presented a diverged event log (peer_role=%s); "+
				"rejecting join, remaining serving master. local_state=%s",
				meshPeerLogID(peerConn), peerRole, state.mode)
			return fmt.Errorf("diverged peer join rejected; this node remains the serving master")
		case handshakeHalted:
			n.log.Warnf("Mesh handshake with peer %s resolved: peer_role=%s progress=%s local_state=%s err=%v",
				meshPeerLogID(peerConn), peerRole, progress, state.mode, state.haltErr)
			if state.haltErr != nil {
				return state.haltErr
			}
			return fmt.Errorf("mesh halted after handshake")
		default:
			return fmt.Errorf("unknown handshake outcome %s", outcome)
		}
	default:
		return fmt.Errorf("handshake resolution reported no outcome (local_state=%s)", state.mode)
	}
}

// postStreamCaughtUpIfAtMasterTip reports completion of initial catch-up
// when the slave reaches the tip carried by the received event.
func (n *node) postStreamCaughtUpIfAtMasterTip(peerConn *nodeConn, entry *eventEnvelope) {
	if entry.Seq != entry.MasterTip || n.control.currentMode() != modeEstablishedSlaveSyncing {
		return
	}
	target := &db.EventLogPosition{
		Seq:     entry.MasterTip,
		TipHash: append([]byte(nil), entry.TipHash...),
	}
	if err := n.control.post(streamCaughtUpSignal{conn: peerConn, target: target}); err != nil {
		n.log.Debugf("failed to post mesh stream caught-up event: %v", err)
	}
}

// postTerminalApplyFailureIfNeeded requests a halt if err is terminal.
func (n *node) postTerminalApplyFailureIfNeeded(err error) {
	if isTerminalEventApplyFailure(err) {
		_ = n.control.post(terminalApplyFailureSignal{err: err, at: time.Now()})
	}
}

// checkEventPublishAvailable returns nil if the node can publish an event now,
// or an error wrapping ErrUnavailable if it cannot. Only a master can publish
// an event, and only a master with an established stream can publish an event
// originating from a forwarded command.
func (n *node) checkEventPublishAvailable(forwardedCommand bool) error {
	state := n.control.currentState()

	if !(state.mode == modeEstablishedMaster || state.mode == modePreparingMaster) {
		return fmt.Errorf("event publisher unavailable for local apply: %w", ErrUnavailable)
	}

	if !forwardedCommand {
		return nil
	}

	// This check is a sanity check.. redundant with the below checks. Only established
	// master should have a stream.
	if state.mode != modeEstablishedMaster {
		return fmt.Errorf("event publisher unavailable for forwarded command: %w", ErrUnavailable)
	}

	if state.activeConn == nil || state.activeConn.link == nil {
		return fmt.Errorf("event publisher has no active peer for forwarded command: %w", ErrUnavailable)
	}

	if !n.stream.isStreamingTo(state.activeConn.ID()) {
		return fmt.Errorf("no event stream for forwarded command: %w", ErrUnavailable)
	}

	return nil
}

// activePeerForRequest returns the active peer connection if allowed returns
// true for the current mode the node is in. It is used to authorize incoming
// requests. If there is no active connection, or the request is not allowed
// in the current mode, it returns an error.
func (n *node) activePeerForRequest(allowed func(nodeMode) bool) (*nodeConn, error) {
	state := n.control.currentState()
	if !allowed(state.mode) {
		return nil, fmt.Errorf("mesh active peer unavailable in node mode %s", state.mode)
	}
	if state.activeConn == nil || state.activeConn.link == nil {
		return nil, fmt.Errorf("mesh active peer connection unavailable")
	}
	return state.activeConn, nil
}

// activePeerForCommandForward returns the active peer connection if the
// current mode allows command forwarding. If there is no active connection,
// or if command forwarding is not allowed in the current mode, it returns an
// error.
func (n *node) activePeerForCommandForward(kind string) (*nodeConn, *msgjson.Error) {
	state := n.control.currentState()
	if !state.mode.canForwardCommands() || state.activeConn == nil || state.activeConn.link == nil {
		return nil, msgjson.NewError(msgjson.TryAgainLaterError,
			"mesh command %q cannot be forwarded; retry the request", kind)
	}
	return state.activeConn, nil
}

// canExecuteCommandLocally reports if the current mode allows command
// execution locally.
func (n *node) canExecuteCommandLocally() bool {
	return n.control.currentMode().canExecuteCommands()
}

// canForwardCommand reports if the current mode allows command forwarding.
func (n *node) canForwardCommand() bool {
	state := n.control.currentState()
	return state.mode.canForwardCommands() && state.activeConn != nil && state.activeConn.link != nil
}

// forwardCommand forwards a prepared state-changing client command from a
// slave to the current master.
func (n *node) forwardCommand(ctx context.Context, cmd *commandForward) (*msgjson.Error, bool) {
	conn, msgErr := n.activePeerForCommandForward(cmd.Kind)
	if msgErr != nil {
		return msgErr, false
	}

	// Use a default request timeout shorter than the pending-command
	// timeout so a late completion can still find the pending entry.
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, forwardCommandTimeout)
		defer cancel()
	}

	if err := conn.link.Request(ctx, commandForwardRoute, cmd, nil); err != nil {
		n.log.Debugf("Command %q forward to master failed: %v", cmd.Kind, err)

		// peerRPCError means that the request reached the master, but was
		// rejected.
		var peerErr *peerRPCError
		if errors.As(err, &peerErr) {
			// UnauthorizedConnection is returned when the request is rejected
			// because master not in the correct mode to execute a forwarded
			// command. This means the client should TryAgainLater.
			if peerErr.Code != msgjson.UnauthorizedConnection {
				return peerErr.MsgError(), false
			}
			return msgjson.NewError(msgjson.TryAgainLaterError,
				"mesh command %q forward refused by a mesh gate; retry the request", cmd.Kind), false
		}

		// No response. The command may have reached the master anyway.
		return nil, true
	}

	return nil, false
}

// sendCommandFailure sends a terminal failure for an accepted forwarded command
// from the master to the slave holding the original client request.
func (n *node) sendCommandFailure(ctx context.Context, fail *commandFailure) error {
	conn, err := n.activePeerForRequest(nodeMode.canDeliverCommandCompletions)
	if err != nil {
		return fmt.Errorf("mesh command failure: %w", err)
	}
	return conn.link.Request(ctx, commandFailureRoute, fail, nil)
}

// sendCommandResult sends a success result for an accepted forwarded command
// from the master to the slave holding the original client request.
func (n *node) sendCommandResult(ctx context.Context, result *commandResult) error {
	conn, err := n.activePeerForRequest(nodeMode.canDeliverCommandCompletions)
	if err != nil {
		return fmt.Errorf("mesh command result: %w", err)
	}
	return conn.link.Request(ctx, commandResultRoute, result, nil)
}

// notifyMasterReady sends a signal to the control loop to indicate
// the master is ready to serve clients.
func (n *node) notifyMasterReady() error {
	res, err := n.control.send(masterReadySignal{})
	if err != nil {
		return err
	}
	if res.err != nil {
		return res.err
	}
	if res.state.mode != modeEstablishedMaster {
		return fmt.Errorf("master readiness resolved to node mode %s", res.state.mode)
	}
	return nil
}

// notifyMasterPreparationFailed sends a signal to the control loop to indicate
// the master preparation failed.
func (n *node) notifyMasterPreparationFailed(err error) error {
	if err == nil {
		err = fmt.Errorf("master preparation failed")
	}
	res, sendErr := n.control.send(masterPreparationFailedSignal{
		err: err,
		at:  time.Now(),
	})
	if sendErr != nil {
		return sendErr
	}
	return res.err
}
