// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// MasterWorker is an application worker that is run while the node is the
// master. These emit events that are not initiated by a client, such as
// progressing markets, revoking orders, etc.
type MasterWorker struct {
	Name string
	// Run starts the worker. Call reportReady with nil once the worker is ready.
	// A non-nil error, or returning without reporting, halts the node. Extra calls
	// are ignored.
	Run func(ctx context.Context, reportReady func(error))
}

// StateLoader loads state from the database into memory.
type StateLoader struct {
	Name string
	Load func(context.Context) error
}

// commandTransport routes state-changing client commands between mesh peers:
// slave-to-master forwarding and master-to-slave completion delivery.
type commandTransport interface {
	canExecuteCommandLocally() bool
	canForwardCommand() bool
	forwardCommand(context.Context, *commandForward) (msgErr *msgjson.Error, outcomeUnknown bool)
	sendCommandFailure(context.Context, *commandFailure) error
	sendCommandResult(context.Context, *commandResult) error
}

// meshTransport is the exposed surface of the mesh node. It is
// implemented by both node and singleServerTransport (which contains
// mostly no-ops).
type meshTransport interface {
	commandTransport

	connect(context.Context) (*sync.WaitGroup, error)
	notifyMasterReady() error
	notifyMasterPreparationFailed(error) error
	ensureSeeded(context.Context) error
	notifyReadyForEvents()
	haltStatus() (bool, error)
	drainEventStream(context.Context) (bool, error)
	requestMasterHandoff(context.Context) error
	checkEventPublishAvailable(forwardedCommand bool) error
	notifyLocalEventCommitted(seq uint64, originCommandID string, commandResult json.RawMessage)
	postTerminalApplyFailureIfNeeded(error)
	sendClientProxyMessage(context.Context, *ClientProxyMessage) error
	queryClientConnected(context.Context, []account.AccountID) ([]account.AccountID, error)
	fillStatus(*Status)
}

// ServiceConfig configures the mesh service. To run in single-server mode,
// leave ListenAddr and PeerAddr empty.
type ServiceConfig struct {
	// DataDir is the directory to store persistent mesh node state.
	DataDir string

	// ListenAddr is the address to listen for incoming mesh connections.
	// Empty in single-server mode.
	ListenAddr string

	// PeerAddr is the address of the peer to connect to.
	// Empty in single-server mode.
	PeerAddr string

	// PeerCert is required for a TLS-enabled peer. It can be empty for non-TLS.
	PeerCert []byte

	// RPCKey is the path to the TLS private key for the local mesh listener.
	RPCKey string

	// RPCCert is the path to the TLS certificate for the local mesh listener.
	RPCCert string

	// NoTLS disables TLS on the local mesh listener.
	NoTLS bool

	// Compat contains the configuration and hash compared during handshake.
	Compat *CompatSnapshot

	// DEXPrivKey is the server's client-facing signing identity. Both mesh
	// nodes must share this key.
	DEXPrivKey *secp256k1.PrivateKey

	// SnapshotStore is the persistence interface for snapshot storage and
	// loading.
	SnapshotStore db.SnapshotStore

	// EventLogReader is the interface for querying the event log.
	EventLogReader db.EventLogReader

	// Logger is the logger for the mesh service.
	Logger dex.Logger

	// ClientHost is this node's client facing address, advertised to the peer.
	ClientHost string

	// ClientCert is this node's client facing TLS certificate, advertised
	// to the peer.
	ClientCert []byte

	// Commands map command types to their executors.
	Commands map[string]CommandExecutor

	// Events map event types to their appliers.
	Events map[string]EventApplier

	// MasterWorkers run while the node is the master (in single-server mode,
	// always). They are started one at a time in registration order, after the
	// state loaders have completed; each must report ready before the next
	// starts.
	MasterWorkers []MasterWorker

	// StateLoaders load state from the database into memory. They run once, in
	// registration order, during startup, after any required snapshot seed (a
	// brand-new node seeds from the peer first). A loader error fails startup.
	StateLoaders []StateLoader

	// ClientProxyHandler is called when the other mesh server asks us to
	// deliver a client message. If the message is for one user and that user
	// is not connected here, return ErrClientNotConnected.
	ClientProxyHandler func(context.Context, *ClientProxyMessage) error

	// ClientConnectedHandler answers the peer's connectivity query: return
	// the subset of users currently connected to this node.
	ClientConnectedHandler func(users []account.AccountID) []account.AccountID

	// PeerClientEndpointChanged is called with the peer's advertised
	// client-facing host/cert at each handshake adoption.
	PeerClientEndpointChanged func(string, []byte)

	// OnHalt is called when a live mesh (after startup has completed) halts.
	OnHalt func(error)
}

func (cfg *ServiceConfig) validate() error {
	if cfg == nil {
		return fmt.Errorf("nil mesh service config")
	}
	if cfg.EventLogReader == nil {
		return fmt.Errorf("event log reader is required")
	}
	if cfg.OnHalt == nil {
		return fmt.Errorf("OnHalt handler is required")
	}

	transportEnabled := cfg.ListenAddr != "" || cfg.PeerAddr != ""
	allTransportConfigDefined := cfg.ClientHost != "" && cfg.ListenAddr != "" && cfg.PeerAddr != ""
	if transportEnabled && !allTransportConfigDefined {
		return fmt.Errorf("client host, listen addr, and peer addr are required when mesh transport is enabled")
	}

	return nil
}

// Service is the mesh service: the application-facing surface over the mesh transport.
type Service struct {
	log dex.Logger

	transport      meshTransport
	eventLogReader db.EventLogReader

	commands *commandCoordinator
	events   map[string]EventApplier

	clientProxyHandler     func(context.Context, *ClientProxyMessage) error
	clientConnectedHandler func(users []account.AccountID) []account.AccountID

	masterWorkers []MasterWorker

	workers *workerSupervisor

	stateLoaders []StateLoader
	loadOnce     sync.Once
	loaded       *readiness

	lifeCtx    context.Context
	lifeCancel context.CancelFunc
	runWG      sync.WaitGroup

	// ready resolves once client comms can start (or startup failed).
	ready *readiness

	onHalt   func(error)
	haltOnce sync.Once

	applyMtx           sync.Mutex
	eventPublishClosed bool
}

var _ meshApplication = (*Service)(nil)

// NewService creates the mesh service.
func NewService(cfg *ServiceConfig) (*Service, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	log := cfg.Logger
	if log == nil {
		log = dex.Disabled
	}

	s := &Service{
		log:                    log,
		eventLogReader:         cfg.EventLogReader,
		events:                 cfg.Events,
		clientProxyHandler:     cfg.ClientProxyHandler,
		clientConnectedHandler: cfg.ClientConnectedHandler,
		masterWorkers:          append([]MasterWorker(nil), cfg.MasterWorkers...),
		stateLoaders:           append([]StateLoader(nil), cfg.StateLoaders...),
		loaded:                 newReadiness(),
		onHalt:                 cfg.OnHalt,
		ready:                  newReadiness(),
	}

	// Single server mode
	transportEnabled := cfg.ListenAddr != "" || cfg.PeerAddr != ""
	if !transportEnabled {
		transport := newSingleServerTransport()
		transport.becameMaster = func() { s.workers.startWorkers() }
		s.transport = transport
		s.commands = newCommandCoordinator(log, transport, "", cfg.Commands, s.applyEvent)
		return s, nil
	}

	frontier, err := cfg.EventLogReader.EventLogFrontier(context.Background())
	if err != nil {
		return nil, fmt.Errorf("event log frontier: %w", err)
	}

	nodeCfg := &nodeConfig{
		dataDir:        cfg.DataDir,
		listenAddr:     cfg.ListenAddr,
		peerAddr:       cfg.PeerAddr,
		peerCert:       cfg.PeerCert,
		rpcKey:         cfg.RPCKey,
		rpcCert:        cfg.RPCCert,
		noTLS:          cfg.NoTLS,
		compat:         cfg.Compat,
		dexPrivKey:     cfg.DEXPrivKey,
		eventLogReader: cfg.EventLogReader,
		snapshotStore:  cfg.SnapshotStore,
		logger:         log,
		app:            s,
		lifecycle: lifecycleHooks{
			becameMaster:              func() { s.workers.startWorkers() },
			peerClientEndpointChanged: cfg.PeerClientEndpointChanged,
			failPendingCommands:       func(reason string) { s.commands.failAllPending(reason) },
			halted:                    s.notifyHalt,
		},
		initialFrontier: frontier,
	}
	nodeCfg.clientHost = cfg.ClientHost
	nodeCfg.clientCert = append([]byte(nil), cfg.ClientCert...)
	node, err := newNode(nodeCfg)
	if err != nil {
		return nil, err
	}

	s.transport = node
	s.commands = newCommandCoordinator(log, node, node.nodeID, cfg.Commands, s.applyEvent)

	return s, nil
}

// ensureLoaded runs the state loaders once, after any required snapshot
// has been loaded.
func (s *Service) ensureLoaded(ctx context.Context) error {
	s.loadOnce.Do(func() {
		if err := s.transport.ensureSeeded(ctx); err != nil {
			s.loaded.resolve(fmt.Errorf("snapshot seed from mesh peer failed: %w", err))
			return
		}
		err := s.runLoaders(ctx)
		s.loaded.resolve(err)
		if err == nil {
			s.transport.notifyReadyForEvents()
		}
	})
	return s.loaded.wait(ctx)
}

// runLoaders runs the registered state loaders in order, failing fast on the
// first error.
func (s *Service) runLoaders(ctx context.Context) error {
	for _, loader := range s.stateLoaders {
		if loader.Load == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		s.log.Infof("Running mesh state loader %q.", loader.Name)
		if err := loader.Load(ctx); err != nil {
			err = fmt.Errorf("state loader %q: %w", loader.Name, err)
			s.log.Errorf("Mesh state loaders failed: %v", err)
			return err
		}
	}
	s.log.Infof("Mesh state loaders complete.")
	return nil
}

// failStartup resolves comms readiness with a startup failure.
// It replaces the error with the transport's halt reason when the
// transport has halted.
func (s *Service) failStartup(err error) {
	if halted, haltErr := s.transport.haltStatus(); halted {
		err = haltErr
	}
	s.ready.resolve(err)
	s.lifeCancel()
}

// Run starts the mesh and blocks until ctx is canceled or the mesh tears
// down internally (startup failure or halt). Must be called at most once
// per Service. Canceling ctx is a graceful stop and may wait for the peer
// to catch up before returning.
func (s *Service) Run(ctx context.Context) {
	// Deliberately not a child of ctx: the drain must outlive the stop
	// request, with the transport still up.
	lifeCtx, cancelLife := context.WithCancel(context.Background())
	s.lifeCtx, s.lifeCancel = lifeCtx, cancelLife

	var transportWG *sync.WaitGroup
	defer func() {
		cancelLife() // a no-op on failure paths: failStartup already canceled
		if transportWG != nil {
			transportWG.Wait()
		}
		s.runWG.Wait()
	}()

	var ok bool
	if transportWG, ok = s.startup(ctx, lifeCtx); !ok {
		return
	}
	s.serve(ctx, lifeCtx)
}

// startup brings the mesh up far enough to serve. On failure, lifeCtx is already
// canceled via failStartup and the bool is false. On success the returned
// WaitGroup is the transport's and must be waited on after lifeCtx ends.
func (s *Service) startup(ctx, lifeCtx context.Context) (*sync.WaitGroup, bool) {
	// Start the worker supervisor before connect so becameMaster can start workers.
	s.workers = newWorkerSupervisor(s.masterWorkers, s.log, s.ensureLoaded, s.handleMasterWorkerReadiness)
	s.workers.startSupervisor(lifeCtx, &s.runWG)

	startupDone := make(chan struct{})
	defer close(startupDone)
	s.watchStartupStop(ctx, startupDone)

	// Run loading alongside connection setup so it can fetch a snapshot
	// when the peer connects.
	s.runWG.Add(1)
	go func() {
		defer s.runWG.Done()
		if err := s.ensureLoaded(lifeCtx); err != nil {
			s.failStartup(err)
		}
	}()

	// Start the transport
	transportWG, err := s.transport.connect(lifeCtx)
	if err != nil {
		// lifeCtx already canceled means another path failed startup first.
		if lifeCtx.Err() == nil {
			s.log.Errorf("mesh transport startup error: %v", err)
		}
		s.failStartup(fmt.Errorf("mesh transport startup error: %w", err))
		return nil, false
	}

	if err := s.loaded.wait(lifeCtx); err != nil {
		s.failStartup(err)
		return transportWG, false
	}

	return transportWG, true
}

// watchStartupStop fails startup if the caller cancels ctx before startup
// finishes. Once startupDone is closed, cancel is left for serve (which may
// drain) instead of treating it as a startup failure.
func (s *Service) watchStartupStop(ctx context.Context, startupDone <-chan struct{}) {
	s.runWG.Add(1)
	go func() {
		defer s.runWG.Done()
		select {
		case <-ctx.Done():
			select {
			case <-startupDone:
			default:
				s.failStartup(ctx.Err())
			}
		case <-startupDone:
		}
	}()
}

// serve resolves comms readiness, then waits for a caller stop (drain then
// tear down) or an internal teardown (no drain).
func (s *Service) serve(ctx, lifeCtx context.Context) {
	s.ready.resolve(nil)

	select {
	case <-ctx.Done():
		if lifeCtx.Err() == nil {
			s.drain(lifeCtx)
		}
	case <-lifeCtx.Done():
	}
}

// WaitUntilReadyForComms blocks until clients may connect, or returns the
// startup or halt error (clients will never connect).
func (s *Service) WaitUntilReadyForComms(ctx context.Context) error {
	return s.ready.wait(ctx)
}

func (s *Service) handleMasterWorkerReadiness(ctx context.Context, err error) {
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return
		}
		s.reportMeshMasterPreparationFailed(ctx, err)
		return
	}

	if err := s.transport.notifyMasterReady(); err != nil {
		if ctx.Err() != nil {
			return
		}
		s.reportMeshMasterPreparationFailed(ctx,
			fmt.Errorf("mesh master readiness transition failed: %w", err))
	}
}

func (s *Service) reportMeshMasterPreparationFailed(ctx context.Context, err error) {
	s.log.Errorf("%v", err)
	if postErr := s.transport.notifyMasterPreparationFailed(err); postErr != nil && ctx.Err() == nil {
		s.log.Errorf("failed to post mesh master preparation failure: %v", postErr)
		s.lifeCancel()
	}
}

func (s *Service) notifyHalt(err error) {
	if err == nil {
		err = fmt.Errorf("mesh halted")
	}

	// If startup already resolved, this is a no-op.
	s.ready.resolve(fmt.Errorf("mesh halted during startup: %w", err))

	// If startup already resolved with a failure, do nothing.
	if err, _ := s.ready.result(); err != nil {
		return
	}

	// The halt did not happen during startup, so deliver a live halt.
	s.haltOnce.Do(func() {
		s.onHalt(err)
	})
}

// ExecuteCommand runs a command locally or forwards it to the master.
// A command may emit an event or complete without one. Mesh delivers its
// response through req.Respond. If an error is returned, the caller sends
// that error to the client and req.Respond is not called.
func (s *Service) ExecuteCommand(ctx context.Context, req CommandRequest) *msgjson.Error {
	return s.commands.execute(ctx, req)
}

// ApplyEvent originates an event. This is only called by the master (or
// single server). If we are not in single server mode, and the node currently
// cannot publish events, an error is returned.
//
// The return value is whatever was set by the event applier, using SetResult.
// If nothing was set, the result is nil.
func (s *Service) ApplyEvent(ctx context.Context, event *Event) (any, error) {
	return s.applyEvent(ctx, event, plainEventOrigin())
}

// applyEvent applies an event. This is the path used by the master. It
// applies the event, and after successful application notifies the event
// stream of the new event so it can be sent to the slave.
func (s *Service) applyEvent(ctx context.Context, event *Event, origin eventOrigin) (any, error) {
	// Serialize event application and stream notification under
	// the applyMtx.
	result, applyCtx, err := func() (any, *EventApplyContext, error) {
		s.applyMtx.Lock()
		defer s.applyMtx.Unlock()

		if s.eventPublishClosed {
			return nil, nil, fmt.Errorf("mesh master is shutting down: %w", ErrUnavailable)
		}

		if err := s.transport.checkEventPublishAvailable(origin.commandID != ""); err != nil {
			return nil, nil, err
		}

		appliedEvent, applyCtx, err := s.applyEventLocal(ctx, event, nil, origin.collectsAfterCommandResult())
		if err != nil {
			return nil, nil, err
		}

		result := applyCtx.Result()
		if origin.resultFn != nil {
			result = origin.resultFn()
		}

		// If the event is a result of a forwarded command from the slave, serialize
		// the result so it can be sent to the slave.
		var commandResultRaw json.RawMessage
		originCommandID := origin.commandID
		if origin.kind == originForwardedCommand {
			commandResultRaw, err = json.Marshal(result)
			if err != nil {
				// The event is durable; only the result reply is lost. Stream the
				// row as a plain event so the slave still applies it and its
				// pending command expires to a retryable answer.
				s.log.Errorf("Failed to encode %s result for forwarded command %s: %v",
					event.Kind, origin.commandID, err)
				originCommandID = ""
				commandResultRaw = nil
			}
		}

		// Notify that a new event is ready to be sent to the slave.
		s.transport.notifyLocalEventCommitted(appliedEvent.Seq, originCommandID, commandResultRaw)

		return result, applyCtx, nil
	}()
	if err != nil {
		s.transport.postTerminalApplyFailureIfNeeded(err)
		return nil, err
	}

	// Event is durable. Client delivery may perform network I/O, so it runs
	// outside applyMtx; delivery errors are logged, not returned.
	if deliverErr := origin.deliverLocalResult(result); deliverErr != nil {
		if errors.Is(deliverErr, errEncodeCommandResult) {
			s.log.Errorf("Failed to encode %s command result: %v", event.Kind, deliverErr)
		} else {
			s.log.Infof("Unable to send %s command result to client: %v", event.Kind, deliverErr)
		}
	}

	// Run any after-command-result callbacks.
	if applyCtx != nil {
		for _, callback := range applyCtx.afterCommandResult {
			callback(ctx)
		}
	}

	return result, nil
}

// applyReceivedEvent applies an event on a slave that was streamed from the
// master.
func (s *Service) applyReceivedEvent(ctx context.Context, entry *eventEnvelope) error {
	// Determine the event origin. If there is a OriginCommandID on the
	// envelope, it means this event is the result of the local node (slave)
	// forwarding a command to the master.
	origin := plainEventOrigin()
	if entry.OriginCommandID != "" {
		if len(entry.CommandResult) == 0 {
			return fmt.Errorf("event %q for command %s missing command result", entry.Kind, entry.OriginCommandID)
		}
		origin = receivedCommandEventOrigin(entry.OriginCommandID)
	}

	// Skip application if this event is already stored with the same tip
	// hash. A different hash at this sequence is a divergence error.
	applyCtx, err := func() (*EventApplyContext, error) {
		s.applyMtx.Lock()
		defer s.applyMtx.Unlock()

		stored, err := entryAt(ctx, s.eventLogReader, entry.Seq)
		var applyCtx *EventApplyContext
		switch {
		case err != nil:
			err = fmt.Errorf("event log entry %d: %w", entry.Seq, err)
		case stored == nil:
			_, applyCtx, err = s.applyEventLocal(ctx, eventFromEnvelope(entry), &db.EventLogPosition{
				Seq:     entry.Seq,
				TipHash: append([]byte(nil), entry.TipHash...),
			}, origin.collectsAfterCommandResult())
		case !bytes.Equal(stored.TipHash, entry.TipHash):
			err = &db.EventLogDivergenceError{
				Seq:             entry.Seq,
				ExpectedTipHash: append([]byte(nil), entry.TipHash...),
				ActualTipHash:   append([]byte(nil), stored.TipHash...),
				Err:             fmt.Errorf("event log replay tip hash mismatch"),
			}
		}
		return applyCtx, err
	}()
	if err != nil {
		return err
	}

	// Deliver the response to the client, if the command that created this
	// event originated on the local (slave) node.
	if origin.kind == originReceivedCommand {
		if err := s.commands.deliverPending(origin.commandID, entry.CommandResult); err != nil {
			s.log.Debugf("failed to deliver event %s result for command %s: %v", entry.Kind, origin.commandID, err)
		}
	}

	// applyCtx is nil when this seq was already in the log.
	if applyCtx != nil {
		for _, callback := range applyCtx.afterCommandResult {
			callback(ctx)
		}
	}

	return nil
}

// applyEventLocal calls the registered applier and checks that it returned
// an event-log entry on success.
func (s *Service) applyEventLocal(ctx context.Context, event *Event, position *db.EventLogPosition, collectAfter bool) (*db.EventLogEntry, *EventApplyContext, error) {
	applyCtx := &EventApplyContext{
		Context:                   ctx,
		Position:                  position,
		collectAfterCommandResult: collectAfter,
	}
	apply := s.events[event.Kind]
	if apply == nil {
		return nil, applyCtx, fmt.Errorf("unsupported mesh event kind %q", event.Kind)
	}
	entry, err := apply(applyCtx, event)
	if err == nil && entry == nil {
		return nil, applyCtx, fmt.Errorf("applier for event kind %q returned no durable event-log row", event.Kind)
	}
	return entry, applyCtx, err
}

// ProxyClientMessage relays a live client message through the active mesh
// peer. When no peer can relay, it fails with ErrClientProxyUnavailable,
// except broadcasts in single-server mode, which are a silent no-op. A
// unicast whose user is not connected on the peer fails with
// ErrClientNotConnected.
func (s *Service) ProxyClientMessage(ctx context.Context, msg *ClientProxyMessage) error {
	return s.transport.sendClientProxyMessage(ctx, msg)
}

// QueryClientConnected asks the peer which of the listed accounts are
// connected to it. Large lists are split across requests; single-server mode
// returns an empty set.
func (s *Service) QueryClientConnected(ctx context.Context, users []account.AccountID) ([]account.AccountID, error) {
	var connected []account.AccountID
	for start := 0; start < len(users); start += maxClientConnectedUsers {
		chunk, err := s.transport.queryClientConnected(ctx, users[start:min(start+maxClientConnectedUsers, len(users))])
		if err != nil {
			return nil, err
		}
		connected = append(connected, chunk...)
	}
	return connected, nil
}

// answerClientConnected implements the meshApplication interface.
func (s *Service) answerClientConnected(users []account.AccountID) []account.AccountID {
	return s.clientConnectedHandler(users)
}

// handleClientProxyMessage implements the meshApplication interface.
func (s *Service) handleClientProxyMessage(ctx context.Context, msg *ClientProxyMessage) error {
	return s.clientProxyHandler(ctx, msg)
}

// executeForwardedCommand implements the meshApplication interface.
func (s *Service) executeForwardedCommand(ctx context.Context, commandID string, req CommandRequest) *msgjson.Error {
	return s.commands.executeForwarded(ctx, commandID, req)
}

// receiveCommandFailure implements the meshApplication interface.
func (s *Service) receiveCommandFailure(commandID string, msgErr *msgjson.Error) {
	s.commands.receiveForwardedFailure(commandID, msgErr)
}

// receiveCommandResult implements the meshApplication interface.
func (s *Service) receiveCommandResult(commandID string, result json.RawMessage) {
	s.commands.receiveForwardedResult(commandID, result)
}
