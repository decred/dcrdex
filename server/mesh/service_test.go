// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/db"
)

type testTransport struct {
	master                              bool
	slave                               bool
	err                                 error
	owner                               *testService
	peer                                *Service
	connectErr                          error
	eventPublishErr                     error
	notifyMasterReadyCalled             chan struct{}
	notifyMasterPreparationFailedCalled chan error

	connectedQueries  []int // per-request user counts
	queryConnectedErr error

	commandForwards []*commandForward
	commandFailures []*commandFailure
	commandResults  []*commandResult
	committedEvents []*eventEnvelope
	postedEvents    []meshSignal

	drain   func(context.Context) (bool, error)
	handoff func(context.Context) error
}

type testService struct {
	*Service

	testTransport    *testTransport
	appliedEvents    []*eventEnvelope
	executedCommands []CommandRequest
}

func runServiceForTest(ctx context.Context, svc *Service) <-chan struct{} {
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		svc.Run(ctx)
	}()
	return runDone
}

func (f *testTransport) connect(context.Context) (*sync.WaitGroup, error) {
	if f.connectErr != nil {
		return nil, f.connectErr
	}
	return new(sync.WaitGroup), nil
}

func (f *testTransport) ensureSeeded(context.Context) error {
	return nil
}

func (f *testTransport) notifyReadyForEvents() {}

func (f *testTransport) haltStatus() (bool, error) {
	return false, nil
}

func (f *testTransport) notifyMasterReady() error {
	f.master = true
	if f.notifyMasterReadyCalled != nil {
		select {
		case f.notifyMasterReadyCalled <- struct{}{}:
		default:
		}
	}
	return nil
}

func (f *testTransport) notifyMasterPreparationFailed(err error) error {
	if f.notifyMasterPreparationFailedCalled != nil {
		select {
		case f.notifyMasterPreparationFailedCalled <- err:
		default:
		}
	}
	return nil
}

func requireServiceReadyBlocked(t testing.TB, svc *Service, desc string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := svc.WaitUntilReadyForComms(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitUntilReadyForComms returned %v, want timeout while %s", err, desc)
	}
}

// registerPeerService links two test services synchronously. This is only test
// plumbing for exercising service-to-service flow without websocket transport.
func (f *testTransport) registerPeerService(peer *Service) {
	f.peer = peer
}

func (f *testTransport) forwardCommand(ctx context.Context, cmd *commandForward) (*msgjson.Error, bool) {
	var kind string
	if cmd != nil {
		kind = cmd.Kind
	}
	if !f.slave {
		return msgjson.NewError(msgjson.TryAgainLaterError,
			"mesh command %q cannot be forwarded; retry the request", kind), false
	}

	f.commandForwards = append(f.commandForwards, cmd)
	if f.err != nil {
		var peerErr *peerRPCError
		if errors.As(f.err, &peerErr) {
			return peerErr.MsgError(), false
		}
		return nil, true
	}
	if f.peer != nil {
		if msgErr := f.peer.executeForwardedCommand(ctx, cmd.CommandID, CommandRequest{
			Kind: cmd.Kind,
			User: cmd.User,
			Msg:  cmd.Msg,
		}); msgErr != nil {
			return msgErr, false
		}
	}
	return nil, false
}

func (f *testTransport) sendCommandFailure(_ context.Context, fail *commandFailure) error {
	f.commandFailures = append(f.commandFailures, fail)
	if f.err != nil {
		return f.err
	}
	if f.peer != nil {
		f.peer.receiveCommandFailure(fail.CommandID, fail.Error)
	}
	return nil
}

func (f *testTransport) sendCommandResult(_ context.Context, result *commandResult) error {
	f.commandResults = append(f.commandResults, result)
	if f.err != nil {
		return f.err
	}
	if f.peer != nil {
		f.peer.receiveCommandResult(result.CommandID, result.Result)
	}
	return nil
}

func (f *testTransport) sendClientProxyMessage(context.Context, *ClientProxyMessage) error {
	return nil
}

func (f *testTransport) queryClientConnected(_ context.Context, users []account.AccountID) ([]account.AccountID, error) {
	f.connectedQueries = append(f.connectedQueries, len(users))
	if f.queryConnectedErr != nil {
		return nil, f.queryConnectedErr
	}
	// Echo the queried chunk back so callers can verify chunked merging.
	return users, nil
}

func (f *testTransport) notifyLocalEventCommitted(seq uint64, originCommandID string, commandResult json.RawMessage) {
	if f.owner == nil || seq == 0 || seq > uint64(len(f.owner.appliedEvents)) {
		return
	}
	applied := f.owner.appliedEvents[int(seq-1)]
	entry := &eventEnvelope{
		Seq:             seq,
		TipHash:         append([]byte(nil), applied.TipHash...),
		MasterTip:       seq,
		Kind:            applied.Kind,
		OriginCommandID: originCommandID,
		CommandResult:   append([]byte(nil), commandResult...),
		Payload:         append([]byte(nil), applied.Payload...),
	}
	f.committedEvents = append(f.committedEvents, entry)
	if f.peer != nil {
		_ = f.peer.applyReceivedEvent(context.Background(), entry)
	}
}

func (f *testTransport) postTerminalApplyFailureIfNeeded(err error) {
	if isTerminalEventApplyFailure(err) {
		_ = f.postEvent(terminalApplyFailureSignal{err: err, at: time.Now()})
	}
}

func (f *testTransport) postEvent(ev meshSignal) error {
	f.postedEvents = append(f.postedEvents, ev)
	return nil
}

func (f *testTransport) canExecuteCommandLocally() bool {
	return f.master
}

func (f *testTransport) canForwardCommand() bool {
	return f.slave
}

func (f *testTransport) checkEventPublishAvailable(bool) error {
	return f.eventPublishErr
}

func (f *testTransport) requireForwardedCommand(t *testing.T, req CommandRequest) *commandForward {
	t.Helper()
	if len(f.commandForwards) != 1 {
		t.Fatalf("forwarded commands = %d, want 1", len(f.commandForwards))
	}
	cmd := f.commandForwards[0]
	if cmd.CommandID == "" {
		t.Fatalf("forwarded command has empty command id")
	}
	if cmd.Kind != req.Kind || cmd.User != req.User || cmd.Msg != req.Msg {
		t.Fatalf("forwarded command mismatch: %+v", cmd)
	}
	return cmd
}

func (f *testTransport) requireNoForwardedCommands(t *testing.T) {
	t.Helper()
	if len(f.commandForwards) != 0 {
		t.Fatalf("forwarded commands = %d, want 0", len(f.commandForwards))
	}
}

func (f *testTransport) requireCommittedEvent(t *testing.T, originCommandID string) {
	t.Helper()
	if len(f.committedEvents) != 1 {
		t.Fatalf("committed events = %d, want 1", len(f.committedEvents))
	}
	if got := f.committedEvents[0].OriginCommandID; got != originCommandID {
		t.Fatalf("committed origin command id = %q, want %q", got, originCommandID)
	}
}

func (f *testTransport) requireCommandResult(t *testing.T, commandID string) {
	t.Helper()
	if len(f.commandResults) != 1 {
		t.Fatalf("command results = %d, want 1", len(f.commandResults))
	}
	if got := f.commandResults[0].CommandID; got != commandID {
		t.Fatalf("command result id = %q, want %s", got, commandID)
	}
}

func (f *testTransport) requireCommandFailure(t *testing.T, commandID string, code int) {
	t.Helper()
	if len(f.commandFailures) != 1 {
		t.Fatalf("command failures = %d, want 1", len(f.commandFailures))
	}
	fail := f.commandFailures[0]
	if fail.CommandID != commandID || fail.Error.Code != code {
		t.Fatalf("command failure = %+v, want command %s code %d", fail, commandID, code)
	}
}

func newTestService(t *testing.T, exec CommandExecutor, transport *testTransport) *testService {
	t.Helper()
	testSvc := new(testService)
	eventLogReader := new(testEventLogReader)
	var transportIface meshTransport = newSingleServerTransport()
	if transport != nil {
		testSvc.testTransport = transport
		transport.owner = testSvc
		transportIface = transport
	}
	commands := make(map[string]CommandExecutor)
	if exec != nil {
		commands["test"] = func(cmd *CommandContext) *msgjson.Error {
			testSvc.executedCommands = append(testSvc.executedCommands, cmd.Request)
			return exec(cmd)
		}
	}
	eventHandlers := map[string]EventApplier{
		"test": func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
			meta := applyCtx.Position
			var seq uint64
			var tipHash []byte
			if meta != nil {
				seq = meta.Seq
				tipHash = append([]byte(nil), meta.TipHash...)
				if want := testTipHash(seq); !bytes.Equal(tipHash, want) {
					t.Fatalf("apply meta tip hash = %x, want %x", tipHash, want)
				}
			}
			if seq == 0 {
				seq = uint64(len(testSvc.appliedEvents) + 1)
			}
			if tipHash == nil {
				tipHash = testTipHash(seq)
			}
			payload := append([]byte(nil), event.Payload...)
			appliedEvent := &db.EventLogEntry{
				Seq:     seq,
				Kind:    event.Kind,
				Event:   payload,
				TipHash: tipHash,
			}
			testSvc.appliedEvents = append(testSvc.appliedEvents, &eventEnvelope{
				Seq:       appliedEvent.Seq,
				TipHash:   append([]byte(nil), appliedEvent.TipHash...),
				MasterTip: appliedEvent.Seq,
				Kind:      appliedEvent.Kind,
				Payload:   append([]byte(nil), appliedEvent.Event...),
			})
			eventLogReader.mtx.Lock()
			eventLogReader.entries = append(eventLogReader.entries, appliedEvent)
			eventLogReader.mtx.Unlock()
			return appliedEvent, nil
		},
	}
	testSvc.Service = &Service{
		loaded:         newReadiness(),
		transport:      transportIface,
		eventLogReader: eventLogReader,
		events:         eventHandlers,
		log:            dex.Disabled,
		ready:          newReadiness(),
	}
	testSvc.Service.commands = newCommandCoordinator(dex.Disabled, transportIface, "test-node", commands, testSvc.Service.applyEvent)
	return testSvc
}

func testEmitResult(result any) CommandExecutor {
	return func(cmd *CommandContext) *msgjson.Error {
		if err := cmd.Completion.Emit(cmd.Context, &Event{Kind: "test"}, func() any {
			return result
		}); err != nil {
			return msgjson.NewError(msgjson.RPCInternalError, "emit failed: %v", err)
		}
		return nil
	}
}

func (s *testService) requireExecutedCommand(t *testing.T, req CommandRequest) {
	t.Helper()
	if len(s.executedCommands) != 1 {
		t.Fatalf("executed commands = %d, want 1", len(s.executedCommands))
	}
	executed := s.executedCommands[0]
	if executed.Kind != req.Kind || executed.User != req.User || executed.Msg != req.Msg {
		t.Fatalf("executed command mismatch: %+v", executed)
	}
}

func (s *testService) requireNoExecutedCommands(t *testing.T) {
	t.Helper()
	if len(s.executedCommands) != 0 {
		t.Fatalf("executed commands = %d, want 0", len(s.executedCommands))
	}
}

func (s *testService) requireAppliedEvents(t *testing.T, want int) {
	t.Helper()
	if len(s.appliedEvents) != want {
		t.Fatalf("applied events = %d, want %d", len(s.appliedEvents), want)
	}
}

func (s *testService) requireNoAppliedEvents(t *testing.T) {
	t.Helper()
	s.requireAppliedEvents(t, 0)
}

func (s *testService) requireAppliedPayload(t *testing.T, want string) {
	t.Helper()
	s.requireAppliedEvents(t, 1)
	if string(s.appliedEvents[0].Payload) != want {
		t.Fatalf("applied payload = %q, want %s", s.appliedEvents[0].Payload, want)
	}
}

func (s *testService) requirePending(t *testing.T, commandID string) {
	t.Helper()
	s.commands.pendingMtx.Lock()
	defer s.commands.pendingMtx.Unlock()
	if s.commands.pending[commandID] == nil {
		t.Fatalf("pending command %q was not retained", commandID)
	}
}

func (s *testService) requirePendingRemoved(t *testing.T, commandID string) {
	t.Helper()
	s.commands.pendingMtx.Lock()
	defer s.commands.pendingMtx.Unlock()
	if s.commands.pending[commandID] != nil {
		t.Fatalf("pending command %q was not removed", commandID)
	}
}

func (s *testService) requireNoPending(t *testing.T) {
	t.Helper()
	s.commands.pendingMtx.Lock()
	defer s.commands.pendingMtx.Unlock()
	if len(s.commands.pending) != 0 {
		t.Fatalf("pending commands = %d, want 0", len(s.commands.pending))
	}
}

type linkedTestServices struct {
	master *testService
	slave  *testService
}

func newLinkedTestServices(t *testing.T, masterExec CommandExecutor) *linkedTestServices {
	t.Helper()
	masterTransport := &testTransport{master: true}
	slaveTransport := &testTransport{slave: true}
	master := newTestService(t, masterExec, masterTransport)
	slave := newTestService(t, nil, slaveTransport)
	masterTransport.registerPeerService(slave.Service)
	slaveTransport.registerPeerService(master.Service)
	return &linkedTestServices{master: master, slave: slave}
}

func (s *linkedTestServices) forwardCommand(t *testing.T, req CommandRequest) string {
	t.Helper()
	if rpcErr := s.slave.ExecuteCommand(context.Background(), req); rpcErr != nil {
		t.Fatalf("ExecuteCommand error: %v", rpcErr)
	}
	return s.slave.testTransport.requireForwardedCommand(t, req).CommandID
}

func (s *testCommandResponder) requireErrorCode(t *testing.T, want int) {
	t.Helper()
	if len(s.sent) != 1 {
		t.Fatalf("responses = %d, want 1", len(s.sent))
	}
	resp, err := s.sent[0].Response()
	if err != nil {
		t.Fatalf("response decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != want {
		t.Fatalf("response error = %+v, want code %d", resp.Error, want)
	}
}

func (s *testCommandResponder) requireNoResponses(t *testing.T) {
	t.Helper()
	if len(s.sent) != 0 {
		t.Fatalf("responses = %d, want 0", len(s.sent))
	}
}

func requireGeneratedCommandID(t *testing.T, commandID string) {
	t.Helper()
	suffix, ok := strings.CutPrefix(commandID, "test-node-")
	if !ok {
		t.Fatalf("command id = %q, want test-node prefix", commandID)
	}
	if suffix == "" {
		t.Fatalf("command id = %q, want numeric suffix", commandID)
	}
	if _, err := strconv.ParseUint(suffix, 10, 64); err != nil {
		t.Fatalf("command id suffix %q is not numeric: %v", suffix, err)
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func requireExecuteCommandError(t *testing.T, got *msgjson.Error, wantCode int, wantErr *msgjson.Error) {
	t.Helper()
	if wantCode == 0 && wantErr == nil {
		if got != nil {
			t.Fatalf("ExecuteCommand error: %v", got)
		}
		return
	}
	if got == nil {
		t.Fatalf("no error")
	}
	if wantErr != nil && (got.Code != wantErr.Code || got.Message != wantErr.Message) {
		t.Fatalf("error = %v, want %v", got, wantErr)
	}
	if wantCode != 0 && got.Code != wantCode {
		t.Fatalf("error code = %d, want %d", got.Code, wantCode)
	}
}

func TestServiceApplyEvent(t *testing.T) {
	t.Run("rejects when local apply is unavailable", func(t *testing.T) {
		transport := &testTransport{eventPublishErr: errors.New("mesh event publisher unavailable for local apply")}
		svc := newTestService(t, nil, transport)
		apply := svc.events["test"]
		applyCalled := false
		svc.events["test"] = func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
			applyCalled = true
			if applyCtx.Position != nil {
				t.Fatalf("apply position = %+v, want nil", applyCtx.Position)
			}
			return apply(applyCtx, event)
		}

		origin := plainEventOrigin()
		event := &Event{
			Kind:    "test",
			Payload: []byte("rejects when local apply is unavailable"),
		}
		_, err := svc.applyEvent(context.Background(), event, origin)
		if err == nil {
			t.Fatal("apply succeeded, want error")
		}
		if applyCalled {
			t.Fatal("applier was called unexpectedly")
		}
		if got := len(transport.committedEvents); got != 0 {
			t.Fatalf("committed events = %d, want 0", got)
		}
		if got := len(transport.postedEvents); got != 0 {
			t.Fatalf("posted events = %d, want 0", got)
		}
	})

	t.Run("rejects forwarded command in single-server mode", func(t *testing.T) {
		result := map[string]string{"status": "ok"}
		svc := newTestService(t, nil, nil)
		apply := svc.events["test"]
		applyCalled := false
		svc.events["test"] = func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
			applyCalled = true
			if applyCtx.Position != nil {
				t.Fatalf("apply position = %+v, want nil", applyCtx.Position)
			}
			return apply(applyCtx, event)
		}

		responder := new(testCommandResponder)
		req := testCommandRequest(t)
		req.Respond = responder.Send
		completion := newCommandCompletion("cmd-forwarded", req, svc.commands)
		resultCalled := false
		origin := completion.eventOrigin(func() any {
			resultCalled = true
			return result
		})
		event := &Event{
			Kind:    "test",
			Payload: []byte("rejects forwarded command in single-server mode"),
		}
		_, err := svc.applyEvent(context.Background(), event, origin)
		if err == nil {
			t.Fatal("apply succeeded, want error")
		}
		if applyCalled {
			t.Fatal("applier was called unexpectedly")
		}
		if resultCalled {
			t.Fatal("result callback was called unexpectedly")
		}
		responder.requireNoResponses(t)
	})

	t.Run("rejects forwarded command when delivery is unavailable", func(t *testing.T) {
		result := map[string]string{"status": "ok"}
		transport := &testTransport{eventPublishErr: errors.New("mesh event publisher unavailable for command cmd-forwarded")}
		svc := newTestService(t, nil, transport)
		apply := svc.events["test"]
		applyCalled := false
		svc.events["test"] = func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
			applyCalled = true
			if applyCtx.Position != nil {
				t.Fatalf("apply position = %+v, want nil", applyCtx.Position)
			}
			return apply(applyCtx, event)
		}

		responder := new(testCommandResponder)
		req := testCommandRequest(t)
		req.Respond = responder.Send
		completion := newCommandCompletion("cmd-forwarded", req, svc.commands)
		resultCalled := false
		origin := completion.eventOrigin(func() any {
			resultCalled = true
			return result
		})
		event := &Event{
			Kind:    "test",
			Payload: []byte("rejects forwarded command when delivery is unavailable"),
		}
		_, err := svc.applyEvent(context.Background(), event, origin)
		if err == nil {
			t.Fatal("apply succeeded, want error")
		}
		if applyCalled {
			t.Fatal("applier was called unexpectedly")
		}
		if resultCalled {
			t.Fatal("result callback was called unexpectedly")
		}
		responder.requireNoResponses(t)
		if got := len(transport.committedEvents); got != 0 {
			t.Fatalf("committed events = %d, want 0", got)
		}
		if got := len(transport.postedEvents); got != 0 {
			t.Fatalf("posted events = %d, want 0", got)
		}
	})

	t.Run("terminal apply error is reported to transport", func(t *testing.T) {
		terminalErr := &CommittedEventApplyError{
			Applied: &db.EventLogEntry{Seq: 1, Kind: "test", TipHash: testTipHash(1)},
			Err:     errors.New("side effect failed"),
		}
		transport := &testTransport{}
		svc := newTestService(t, nil, transport)
		applyCalled := false
		svc.events["test"] = func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
			applyCalled = true
			if applyCtx.Position != nil {
				t.Fatalf("apply position = %+v, want nil", applyCtx.Position)
			}
			return nil, terminalErr
		}

		origin := plainEventOrigin()
		event := &Event{
			Kind:    "test",
			Payload: []byte("terminal apply error is reported to transport"),
		}
		_, err := svc.applyEvent(context.Background(), event, origin)
		if !errors.Is(err, terminalErr) {
			t.Fatalf("apply error = %v, want %v", err, terminalErr)
		}
		if !applyCalled {
			t.Fatal("applier was not called")
		}
		if got := len(transport.committedEvents); got != 0 {
			t.Fatalf("committed events = %d, want 0", got)
		}
		if got := len(transport.postedEvents); got != 1 {
			t.Fatalf("posted events = %d, want 1", got)
		}
	})

	t.Run("plain event applies in single-server mode", func(t *testing.T) {
		svc := newTestService(t, nil, nil)
		apply := svc.events["test"]
		applyCalled := false
		svc.events["test"] = func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
			applyCalled = true
			if applyCtx.Position != nil {
				t.Fatalf("apply position = %+v, want nil", applyCtx.Position)
			}
			return apply(applyCtx, event)
		}

		origin := plainEventOrigin()
		event := &Event{
			Kind:    "test",
			Payload: []byte("plain event applies in single-server mode"),
		}
		_, err := svc.applyEvent(context.Background(), event, origin)
		if err != nil {
			t.Fatalf("apply error: %v", err)
		}
		if !applyCalled {
			t.Fatal("applier was not called")
		}
	})

	t.Run("plain event applies and notifies in mesh master mode", func(t *testing.T) {
		transport := &testTransport{master: true}
		svc := newTestService(t, nil, transport)
		apply := svc.events["test"]
		applyCalled := false
		svc.events["test"] = func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
			applyCalled = true
			if applyCtx.Position != nil {
				t.Fatalf("apply position = %+v, want nil", applyCtx.Position)
			}
			return apply(applyCtx, event)
		}

		origin := plainEventOrigin()
		event := &Event{
			Kind:    "test",
			Payload: []byte("plain event applies and notifies in mesh master mode"),
		}
		_, err := svc.applyEvent(context.Background(), event, origin)
		if err != nil {
			t.Fatalf("apply error: %v", err)
		}
		if !applyCalled {
			t.Fatal("applier was not called")
		}
		if got := len(transport.committedEvents); got != 1 {
			t.Fatalf("committed events = %d, want 1", got)
		}
		entry := transport.committedEvents[0]
		if entry.OriginCommandID != "" {
			t.Fatalf("origin command id = %q, want %q", entry.OriginCommandID, "")
		}
		if len(entry.CommandResult) != 0 {
			t.Fatalf("command result = %q, want empty", entry.CommandResult)
		}
		if got := len(transport.postedEvents); got != 0 {
			t.Fatalf("posted events = %d, want 0", got)
		}
	})

	t.Run("local command result is delivered after apply", func(t *testing.T) {
		result := map[string]string{"status": "ok"}
		svc := newTestService(t, nil, nil)
		apply := svc.events["test"]
		applyCalled := false
		svc.events["test"] = func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
			applyCalled = true
			if applyCtx.Position != nil {
				t.Fatalf("apply position = %+v, want nil", applyCtx.Position)
			}
			return apply(applyCtx, event)
		}

		responder := new(testCommandResponder)
		req := testCommandRequest(t)
		req.Respond = responder.Send
		completion := newCommandCompletion("", req, svc.commands)
		resultCalled := false
		origin := completion.eventOrigin(func() any {
			resultCalled = true
			return result
		})
		event := &Event{
			Kind:    "test",
			Payload: []byte("local command result is delivered after apply"),
		}
		_, err := svc.applyEvent(context.Background(), event, origin)
		if err != nil {
			t.Fatalf("apply error: %v", err)
		}
		if !applyCalled {
			t.Fatal("applier was not called")
		}
		if !resultCalled {
			t.Fatal("result callback was not called")
		}
		responder.requireResult(t, result)
	})

	t.Run("forwarded command result is attached to committed event", func(t *testing.T) {
		result := map[string]string{"status": "ok"}
		transport := &testTransport{master: true}
		svc := newTestService(t, nil, transport)
		apply := svc.events["test"]
		applyCalled := false
		svc.events["test"] = func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
			applyCalled = true
			if applyCtx.Position != nil {
				t.Fatalf("apply position = %+v, want nil", applyCtx.Position)
			}
			return apply(applyCtx, event)
		}

		responder := new(testCommandResponder)
		req := testCommandRequest(t)
		req.Respond = responder.Send
		completion := newCommandCompletion("cmd-forwarded", req, svc.commands)
		resultCalled := false
		origin := completion.eventOrigin(func() any {
			resultCalled = true
			return result
		})
		event := &Event{
			Kind:    "test",
			Payload: []byte("forwarded command result is attached to committed event"),
		}
		_, err := svc.applyEvent(context.Background(), event, origin)
		if err != nil {
			t.Fatalf("apply error: %v", err)
		}
		if !applyCalled {
			t.Fatal("applier was not called")
		}
		if !resultCalled {
			t.Fatal("result callback was not called")
		}
		responder.requireNoResponses(t)
		if got := len(transport.committedEvents); got != 1 {
			t.Fatalf("committed events = %d, want 1", got)
		}
		entry := transport.committedEvents[0]
		if entry.OriginCommandID != "cmd-forwarded" {
			t.Fatalf("origin command id = %q, want %q", entry.OriginCommandID, "cmd-forwarded")
		}
		requireJSONResult(t, entry.CommandResult, result)
		if got := len(transport.postedEvents); got != 0 {
			t.Fatalf("posted events = %d, want 0", got)
		}
	})

	t.Run("forwarded command result marshal error commits without a command result", func(t *testing.T) {
		transport := &testTransport{master: true}
		svc := newTestService(t, nil, transport)
		apply := svc.events["test"]
		applyCalled := false
		svc.events["test"] = func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
			applyCalled = true
			if applyCtx.Position != nil {
				t.Fatalf("apply position = %+v, want nil", applyCtx.Position)
			}
			return apply(applyCtx, event)
		}

		responder := new(testCommandResponder)
		req := testCommandRequest(t)
		req.Respond = responder.Send
		completion := newCommandCompletion("cmd-forwarded", req, svc.commands)
		resultCalled := false
		origin := completion.eventOrigin(func() any {
			resultCalled = true
			return make(chan int)
		})
		event := &Event{
			Kind:    "test",
			Payload: []byte("forwarded command result marshal error commits without a command result"),
		}
		_, err := svc.applyEvent(context.Background(), event, origin)
		if err != nil {
			t.Fatalf("apply error: %v", err)
		}
		if !applyCalled {
			t.Fatal("applier was not called")
		}
		if !resultCalled {
			t.Fatal("result callback was not called")
		}
		responder.requireNoResponses(t)
		if got := len(transport.committedEvents); got != 1 {
			t.Fatalf("committed events = %d, want 1", got)
		}
		entry := transport.committedEvents[0]
		if entry.OriginCommandID != "" {
			t.Fatalf("origin command id = %q, want %q", entry.OriginCommandID, "")
		}
		if len(entry.CommandResult) != 0 {
			t.Fatalf("command result = %q, want empty", entry.CommandResult)
		}
		if got := len(transport.postedEvents); got != 0 {
			t.Fatalf("posted events = %d, want 0", got)
		}
	})

	t.Run("apply without durable row fails", func(t *testing.T) {
		transport := &testTransport{master: true}
		svc := newTestService(t, nil, transport)
		applyCalled := false
		svc.events["test"] = func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
			applyCalled = true
			if applyCtx.Position != nil {
				t.Fatalf("apply position = %+v, want nil", applyCtx.Position)
			}
			return nil, nil
		}

		origin := plainEventOrigin()
		event := &Event{
			Kind:    "test",
			Payload: []byte("apply without durable row fails"),
		}
		_, err := svc.applyEvent(context.Background(), event, origin)
		if err == nil {
			t.Fatal("apply succeeded, want error")
		}
		if !applyCalled {
			t.Fatal("applier was not called")
		}
		if got := len(transport.committedEvents); got != 0 {
			t.Fatalf("committed events = %d, want 0", got)
		}
		if got := len(transport.postedEvents); got != 0 {
			t.Fatalf("posted events = %d, want 0", got)
		}
	})
}

func TestServiceApplyEventDeliversApplierSetResult(t *testing.T) {
	result := map[string]string{"status": "applied"}
	responder := &testCommandResponder{}
	req := testCommandRequest(t)
	req.Respond = responder.Send

	svc := newTestService(t, nil, nil)
	completion := newCommandCompletion("", req, svc.commands)

	svc.events["test"] = func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
		applyCtx.SetResult(result)
		return &db.EventLogEntry{
			Seq:     1,
			Kind:    event.Kind,
			Event:   append([]byte(nil), event.Payload...),
			TipHash: testTipHash(1),
		}, nil
	}

	// With no result function, the result recorded by the applier through
	// SetResult is delivered as the command result.
	if _, err := svc.applyEvent(context.Background(), &Event{Kind: "test"}, completion.eventOrigin(nil)); err != nil {
		t.Fatalf("applyEvent error: %v", err)
	}
	responder.requireResult(t, result)
}

// TestServiceApplyEventReturnsApplierResult verifies the plain-event path: the
// value an applier records with SetResult is returned to the local emitter
// from ApplyEvent, so emitters can consume applier-derived state without a
// side channel.
func TestServiceApplyEventReturnsApplierResult(t *testing.T) {
	result := map[string]string{"status": "applied"}

	svc := newTestService(t, nil, nil)
	svc.events["test"] = func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
		applyCtx.SetResult(result)
		return &db.EventLogEntry{
			Seq:     1,
			Kind:    event.Kind,
			Event:   append([]byte(nil), event.Payload...),
			TipHash: testTipHash(1),
		}, nil
	}

	got, err := svc.ApplyEvent(context.Background(), &Event{Kind: "test"})
	if err != nil {
		t.Fatalf("ApplyEvent error: %v", err)
	}
	gotMap, ok := got.(map[string]string)
	if !ok || gotMap["status"] != "applied" {
		t.Fatalf("ApplyEvent result = %v, want %v", got, result)
	}
}

func TestServiceApplyEventIgnoresDeliveryError(t *testing.T) {
	result := map[string]string{"status": "ok"}
	deliverErr := errors.New("delivery failed")
	responder := &testCommandResponder{err: deliverErr}
	req := testCommandRequest(t)

	svc := newTestService(t, nil, nil)
	req.Respond = func(msg *msgjson.Message) error {
		if !svc.applyMtx.TryLock() {
			t.Fatal("applyMtx held during result delivery")
		}
		svc.applyMtx.Unlock()
		return responder.Send(msg)
	}
	completion := newCommandCompletion("", req, svc.commands)

	callbackRan := false
	svc.events["test"] = func(applyCtx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
		if !applyCtx.AfterCommandResult(func(context.Context) {
			callbackRan = true
		}) {
			t.Fatalf("AfterCommandResult returned false")
		}
		return &db.EventLogEntry{
			Seq:     1,
			Kind:    event.Kind,
			Event:   append([]byte(nil), event.Payload...),
			TipHash: testTipHash(1),
		}, nil
	}

	_, err := svc.applyEvent(context.Background(), &Event{Kind: "test"}, completion.eventOrigin(func() any {
		return result
	}))
	if err != nil {
		t.Fatalf("applyEvent error = %v, want nil for post-commit delivery failure", err)
	}
	if !callbackRan {
		t.Fatalf("after-command-result callback did not run")
	}
	responder.requireResult(t, result)
}

func TestServiceApplyEventLocal(t *testing.T) {
	svc := newTestService(t, nil, nil)

	if _, _, err := svc.applyEventLocal(context.Background(), &Event{Kind: "test", Payload: []byte("applied")}, nil, false); err != nil {
		t.Fatalf("applyEventLocal error: %v", err)
	}
	svc.requireAppliedPayload(t, "applied")

	if _, _, err := svc.applyEventLocal(context.Background(), &Event{Kind: "unknown"}, nil, false); err == nil {
		t.Fatalf("no error for unknown event kind")
	}
}

func TestServiceExecuteCommand(t *testing.T) {
	wantForwardErr := msgjson.NewError(msgjson.AuthenticationError, "nope")
	wantForwardPeerErr := &peerRPCError{Code: msgjson.AuthenticationError, Message: "nope"}
	tests := []struct {
		name                string
		transport           *testTransport
		mutateReq           func(*CommandRequest)
		wantErrCode         int
		wantErrIs           *msgjson.Error
		wantExecutedLocally bool
		wantForward         bool
	}{
		{
			name:                "single-server executes locally",
			wantExecutedLocally: true,
		},
		{
			name:                "master executes locally",
			transport:           &testTransport{master: true},
			wantExecutedLocally: true,
		},
		{
			name:        "slave forwards",
			transport:   &testTransport{slave: true},
			wantForward: true,
		},
		{
			name:        "unavailable mesh rejects retryably",
			transport:   &testTransport{},
			wantErrCode: msgjson.TryAgainLaterError,
		},
		{
			name:        "forward transport error holds pending",
			transport:   &testTransport{slave: true, err: errors.New("boom")},
			wantForward: true,
		},
		{
			name:        "forward preserves peer app-level error",
			transport:   &testTransport{slave: true, err: wantForwardPeerErr},
			wantErrIs:   wantForwardErr,
			wantForward: true,
		},
		{
			name:        "nil command message",
			mutateReq:   func(req *CommandRequest) { req.Msg = nil },
			wantErrCode: msgjson.RPCInternalError,
		},
		{
			name:        "nil command responder",
			mutateReq:   func(req *CommandRequest) { req.Respond = nil },
			wantErrCode: msgjson.RPCInternalError,
		},
		{
			name:        "unknown local command",
			mutateReq:   func(req *CommandRequest) { req.Kind = "unknown" },
			wantErrCode: msgjson.RPCInternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := testCommandRequest(t)
			responder := new(testCommandResponder)
			req.Respond = responder.Send
			if tt.mutateReq != nil {
				tt.mutateReq(&req)
			}

			result := map[string]string{"status": "ok"}
			svc := newTestService(t, testEmitResult(result), tt.transport)

			rpcErr := svc.ExecuteCommand(context.Background(), req)
			requireExecuteCommandError(t, rpcErr, tt.wantErrCode, tt.wantErrIs)

			if tt.wantExecutedLocally {
				svc.requireExecutedCommand(t, req)
				svc.requireAppliedEvents(t, 1)
				responder.requireResult(t, result)
			} else {
				svc.requireNoExecutedCommands(t)
				svc.requireNoAppliedEvents(t)
				responder.requireNoResponses(t)
			}

			if tt.transport == nil {
				svc.requireNoPending(t)
				return
			}
			if !tt.wantForward {
				tt.transport.requireNoForwardedCommands(t)
				svc.requireNoPending(t)
				return
			}

			commandID := tt.transport.requireForwardedCommand(t, req).CommandID
			requireGeneratedCommandID(t, commandID)
			wantPending := tt.wantErrCode == 0 && tt.wantErrIs == nil
			if wantPending {
				svc.requirePending(t, commandID)
			} else {
				svc.requirePendingRemoved(t, commandID)
			}
			svc.commands.removePending(commandID)
		})
	}
}

func TestServiceReceivedEventReplay(t *testing.T) {
	svc := newTestService(t, nil, &testTransport{slave: true})
	applyStarted := make(chan struct{})
	releaseApply := make(chan struct{})
	originalApply := svc.events["test"]
	var applyCalls atomic.Int32
	svc.events["test"] = func(ctx *EventApplyContext, event *Event) (*db.EventLogEntry, error) {
		applyCalls.Add(1)
		close(applyStarted)
		<-releaseApply
		return originalApply(ctx, event)
	}
	env := &eventEnvelope{Seq: 1, TipHash: testTipHash(1), Kind: "test"}

	firstDone := make(chan error, 1)
	go func() { firstDone <- svc.applyReceivedEvent(context.Background(), env) }()
	<-applyStarted
	commandResult := map[string]string{"status": "accepted"}
	responder := new(testCommandResponder)
	req := testCommandRequest(t)
	req.Respond = responder.Send
	svc.commands.registerPending("cmd-replay", req)
	replay := *env
	replay.OriginCommandID = "cmd-replay"
	replay.CommandResult = mustMarshalJSON(t, commandResult)
	secondDone := make(chan error, 1)
	go func() { secondDone <- svc.applyReceivedEvent(context.Background(), &replay) }()
	select {
	case err := <-secondDone:
		t.Fatalf("replay returned before the original apply completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseApply)
	for i, done := range []<-chan error{firstDone, secondDone} {
		if err := <-done; err != nil {
			t.Fatalf("apply %d: %v", i+1, err)
		}
	}
	if calls := applyCalls.Load(); calls != 1 {
		t.Fatalf("applier calls = %d, want 1", calls)
	}
	responder.requireResult(t, commandResult)
	svc.requirePendingRemoved(t, replay.OriginCommandID)

	diverged := *env
	diverged.TipHash = testTipHash(2)
	err := svc.applyReceivedEvent(context.Background(), &diverged)
	var divergence *db.EventLogDivergenceError
	if !errors.As(err, &divergence) {
		t.Fatalf("divergent replay error = %T %[1]v, want EventLogDivergenceError", err)
	}
}

func TestServiceForwardedCommandLifecycle(t *testing.T) {
	t.Run("event backed command publishes event and slave delivers result", func(t *testing.T) {
		req := testCommandRequest(t)
		responder := new(testCommandResponder)
		req.Respond = responder.Send
		result := map[string]string{"status": "accepted"}
		services := newLinkedTestServices(t, func(cmd *CommandContext) *msgjson.Error {
			if err := cmd.Completion.Emit(cmd.Context, &Event{
				Kind:    "test",
				Payload: []byte("accepted"),
			}, func() any {
				return result
			}); err != nil {
				return msgjson.NewError(msgjson.RPCInternalError, "emit failed: %v", err)
			}
			return nil
		})

		commandID := services.forwardCommand(t, req)
		services.master.requireExecutedCommand(t, req)
		services.master.requireAppliedEvents(t, 1)

		services.master.testTransport.requireCommittedEvent(t, commandID)
		entry := services.master.testTransport.committedEvents[0]
		if entry.OriginCommandID != commandID {
			t.Fatalf("origin command id = %q, want %s", entry.OriginCommandID, commandID)
		}
		if len(entry.CommandResult) == 0 {
			t.Fatalf("missing forwarded command result")
		}
		services.slave.requireAppliedPayload(t, "accepted")
		responder.requireResult(t, result)
		services.slave.requirePendingRemoved(t, commandID)
	})

	t.Run("no event command sends command result and slave delivers result", func(t *testing.T) {
		req := testCommandRequest(t)
		responder := new(testCommandResponder)
		req.Respond = responder.Send
		result := map[string]string{"status": "already"}
		services := newLinkedTestServices(t, func(cmd *CommandContext) *msgjson.Error {
			if err := cmd.Completion.Complete(cmd.Context, result); err != nil {
				return msgjson.NewError(msgjson.RPCInternalError, "complete failed: %v", err)
			}
			return nil
		})

		commandID := services.forwardCommand(t, req)
		services.master.requireExecutedCommand(t, req)
		services.master.requireNoAppliedEvents(t)
		services.master.testTransport.requireCommandResult(t, commandID)
		responder.requireResult(t, result)
		services.slave.requirePendingRemoved(t, commandID)
	})

	t.Run("event with origin command id requires command result", func(t *testing.T) {
		slave := newTestService(t, nil, &testTransport{slave: true})
		missingResult := &eventEnvelope{Seq: 2, TipHash: testTipHash(2), MasterTip: 2, Kind: "test", OriginCommandID: "cmd-missing-result"}
		if err := slave.applyReceivedEvent(context.Background(), missingResult); err == nil {
			t.Fatalf("missing command result did not error")
		}
		slave.requireNoAppliedEvents(t)
	})

	t.Run("received event delivers command result", func(t *testing.T) {
		slave := newTestService(t, nil, &testTransport{slave: true})
		commandID := "cmd-event-result"
		req := testCommandRequest(t)
		responder := new(testCommandResponder)
		req.Respond = func(msg *msgjson.Message) error {
			if !slave.applyMtx.TryLock() {
				t.Fatal("applyMtx held during received result delivery")
			}
			slave.applyMtx.Unlock()
			return responder.Send(msg)
		}
		slave.commands.registerPending(commandID, req)
		defer slave.commands.removePending(commandID)

		commandResult := map[string]string{"status": "accepted"}
		err := slave.applyReceivedEvent(context.Background(), &eventEnvelope{
			Seq:             1,
			TipHash:         testTipHash(1),
			MasterTip:       1,
			Kind:            "test",
			OriginCommandID: commandID,
			CommandResult:   mustMarshalJSON(t, commandResult),
			Payload:         []byte("accepted"),
		})
		if err != nil {
			t.Fatalf("applyReceivedEvent error: %v", err)
		}
		slave.requireAppliedPayload(t, "accepted")
		responder.requireResult(t, commandResult)
		slave.requirePendingRemoved(t, commandID)
	})

	t.Run("received event treats missing pending command as no-op", func(t *testing.T) {
		slave := newTestService(t, nil, &testTransport{slave: true})
		err := slave.applyReceivedEvent(context.Background(), &eventEnvelope{
			Seq:             1,
			TipHash:         testTipHash(1),
			MasterTip:       1,
			Kind:            "test",
			OriginCommandID: "cmd-not-pending",
			CommandResult:   mustMarshalJSON(t, map[string]string{"status": "accepted"}),
		})
		if err != nil {
			t.Fatalf("applyReceivedEvent error: %v", err)
		}
		slave.requireAppliedEvents(t, 1)
		slave.requireNoPending(t)
	})

	t.Run("received event ignores command result delivery error", func(t *testing.T) {
		slave := newTestService(t, nil, &testTransport{slave: true})
		commandID := "cmd-bad-result"
		req := testCommandRequest(t)
		responder := new(testCommandResponder)
		req.Respond = responder.Send
		slave.commands.registerPending(commandID, req)
		defer slave.commands.removePending(commandID)

		err := slave.applyReceivedEvent(context.Background(), &eventEnvelope{
			Seq:             1,
			TipHash:         testTipHash(1),
			MasterTip:       1,
			Kind:            "test",
			OriginCommandID: commandID,
			CommandResult:   json.RawMessage(`{`),
		})
		if err != nil {
			t.Fatalf("applyReceivedEvent error: %v", err)
		}
		responder.requireNoResponses(t)
		slave.requirePendingRemoved(t, commandID)
	})

	t.Run("command failure delivers pending error", func(t *testing.T) {
		req := testCommandRequest(t)
		responder := new(testCommandResponder)
		req.Respond = responder.Send
		services := newLinkedTestServices(t, func(cmd *CommandContext) *msgjson.Error {
			if err := cmd.Completion.Fail(cmd.Context, msgjson.NewError(msgjson.FundingError, "funding failed")); err != nil {
				return msgjson.NewError(msgjson.RPCInternalError, "fail failed: %v", err)
			}
			return nil
		})

		commandID := services.forwardCommand(t, req)
		services.master.requireExecutedCommand(t, req)
		services.master.testTransport.requireCommandFailure(t, commandID, msgjson.FundingError)
		responder.requireErrorCode(t, msgjson.FundingError)
		services.slave.requirePendingRemoved(t, commandID)
	})
}

func TestServiceWaitUntilReadyForComms(t *testing.T) {
	t.Run("single-server worker success owns readiness", func(t *testing.T) {
		ready := make(chan struct{})
		svc, err := NewService(&ServiceConfig{
			EventLogReader: &testEventLogReader{},
			OnHalt:         func(error) {},
			MasterWorkers: []MasterWorker{{
				Name: "test",
				Run: func(ctx context.Context, reportReady func(error)) {
					select {
					case <-ready:
						reportReady(nil)
					case <-ctx.Done():
						return
					}
					<-ctx.Done()
				},
			}},
		})
		if err != nil {
			t.Fatalf("NewService error: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		runDone := runServiceForTest(ctx, svc)

		requireServiceReadyBlocked(t, svc, "single-server worker startup")
		close(ready)
		if err := svc.WaitUntilReadyForComms(context.Background()); err != nil {
			t.Fatalf("WaitUntilReadyForComms error: %v", err)
		}

		cancel()
		select {
		case <-runDone:
		case <-time.After(time.Second):
			t.Fatalf("service did not stop")
		}
	})

	t.Run("single-server worker failure fails readiness", func(t *testing.T) {
		readyErr := errors.New("market startup cleanup failed")
		halted := make(chan error, 1)
		svc, err := NewService(&ServiceConfig{
			EventLogReader: &testEventLogReader{},
			OnHalt: func(err error) {
				halted <- err
			},
			MasterWorkers: []MasterWorker{{
				Name: "test",
				Run: func(ctx context.Context, reportReady func(error)) {
					reportReady(readyErr)
					<-ctx.Done()
				},
			}},
		})
		if err != nil {
			t.Fatalf("NewService error: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		runDone := runServiceForTest(ctx, svc)

		err = svc.WaitUntilReadyForComms(context.Background())
		if !errors.Is(err, readyErr) {
			t.Fatalf("WaitUntilReadyForComms error = %v, want %v", err, readyErr)
		}
		if !strings.Contains(err.Error(), `mesh master worker "test" startup readiness failed`) {
			t.Fatalf("WaitUntilReadyForComms error = %v, missing service wrapper", err)
		}
		select {
		case haltErr := <-halted:
			t.Fatalf("OnHalt called for startup readiness failure: %v", haltErr)
		default:
		}
		select {
		case <-runDone:
		case <-time.After(time.Second):
			t.Fatalf("service did not stop after readiness failure")
		}
	})

	t.Run("mesh transport connect success owns readiness", func(t *testing.T) {
		svc := &Service{
			loaded:    newReadiness(),
			log:       dex.Disabled,
			transport: &testTransport{},
			ready:     newReadiness(),
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		runDone := runServiceForTest(ctx, svc)

		if err := svc.WaitUntilReadyForComms(context.Background()); err != nil {
			t.Fatalf("WaitUntilReadyForComms error: %v", err)
		}

		cancel()
		select {
		case <-runDone:
		case <-time.After(time.Second):
			t.Fatalf("service did not stop")
		}
	})

	t.Run("mesh transport connect error fails readiness", func(t *testing.T) {
		connectErr := errors.New("connect failed")
		halted := make(chan error, 1)
		svc := &Service{
			loaded:    newReadiness(),
			log:       dex.Disabled,
			transport: &testTransport{connectErr: connectErr},
			ready:     newReadiness(),
			onHalt: func(err error) {
				halted <- err
			},
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		runDone := runServiceForTest(ctx, svc)

		err := svc.WaitUntilReadyForComms(context.Background())
		if !errors.Is(err, connectErr) {
			t.Fatalf("WaitUntilReadyForComms error = %v, want %v", err, connectErr)
		}
		if !strings.Contains(err.Error(), "mesh transport startup error") {
			t.Fatalf("WaitUntilReadyForComms error = %v, missing startup wrapper", err)
		}
		select {
		case haltErr := <-halted:
			t.Fatalf("OnHalt called for transport startup error: %v", haltErr)
		default:
		}
		select {
		case <-runDone:
		case <-time.After(time.Second):
			t.Fatalf("service did not stop after transport startup error")
		}
	})

	t.Run("mesh worker success posts master ready without service readiness", func(t *testing.T) {
		masterReady := make(chan struct{}, 1)
		transport := &testTransport{notifyMasterReadyCalled: masterReady}
		svc := &Service{
			loaded:    newReadiness(),
			log:       dex.Disabled,
			transport: transport,
			masterWorkers: []MasterWorker{{
				Name: "test",
				Run: func(ctx context.Context, reportReady func(error)) {
					reportReady(nil)
					<-ctx.Done()
				},
			}},
			ready: newReadiness(),
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		svc.lifeCtx = ctx
		svc.lifeCancel = cancel
		startTestWorkerSupervisor(svc, ctx)
		svc.workers.startWorkers()

		select {
		case <-masterReady:
		case <-time.After(time.Second):
			t.Fatalf("transport notifyMasterReady was not called")
		}
		if !transport.master {
			t.Fatalf("transport notifyMasterReady was not called")
		}
		requireServiceReadyBlocked(t, svc, "mesh worker success waits for transport connect readiness")

		cancel()
		waitServiceWorkers(t, svc, "mesh worker success shutdown")
	})

	t.Run("mesh worker failure posts preparation failure without service readiness", func(t *testing.T) {
		readyErr := errors.New("market startup cleanup failed")
		prepFailed := make(chan error, 1)
		halted := make(chan error, 1)
		transport := &testTransport{notifyMasterPreparationFailedCalled: prepFailed}
		svc := &Service{
			loaded:    newReadiness(),
			log:       dex.Disabled,
			transport: transport,
			onHalt: func(err error) {
				halted <- err
			},
			masterWorkers: []MasterWorker{{
				Name: "test",
				Run: func(ctx context.Context, reportReady func(error)) {
					reportReady(readyErr)
					<-ctx.Done()
				},
			}},
			ready: newReadiness(),
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		svc.lifeCtx = ctx
		svc.lifeCancel = cancel
		startTestWorkerSupervisor(svc, ctx)
		svc.workers.startWorkers()

		select {
		case err := <-prepFailed:
			if !errors.Is(err, readyErr) {
				t.Fatalf("notifyMasterPreparationFailed error = %v, want %v", err, readyErr)
			}
		case <-time.After(time.Second):
			t.Fatalf("transport notifyMasterPreparationFailed was not called")
		}
		requireServiceReadyBlocked(t, svc, "mesh worker failure waits for transport connect readiness")
		select {
		case haltErr := <-halted:
			t.Fatalf("OnHalt called directly for mesh worker failure: %v", haltErr)
		default:
		}

		cancel()
		waitServiceWorkers(t, svc, "mesh worker failure shutdown")
	})

}

func TestServiceQueryClientConnectedChunking(t *testing.T) {
	transport := &testTransport{}
	svc := &Service{log: dex.Disabled, transport: transport}

	users := make([]account.AccountID, 2*maxClientConnectedUsers+1)
	for i := range users {
		users[i][0], users[i][1], users[i][2] = byte(i), byte(i>>8), byte(i>>16)
	}

	connected, err := svc.QueryClientConnected(context.Background(), users)
	if err != nil {
		t.Fatalf("QueryClientConnected error: %v", err)
	}
	wantChunks := []int{maxClientConnectedUsers, maxClientConnectedUsers, 1}
	if len(transport.connectedQueries) != len(wantChunks) {
		t.Fatalf("requests = %d, want %d", len(transport.connectedQueries), len(wantChunks))
	}
	for i, want := range wantChunks {
		if transport.connectedQueries[i] != want {
			t.Fatalf("request %d queried %d users, want %d", i, transport.connectedQueries[i], want)
		}
	}
	// The echoing test transport claims every queried user is connected, so
	// the merged result must be the full user list in order.
	if len(connected) != len(users) {
		t.Fatalf("connected = %d users, want %d", len(connected), len(users))
	}
	for i, user := range users {
		if connected[i] != user {
			t.Fatalf("connected[%d] = %v, want %v", i, connected[i], user)
		}
	}

	// A chunk failure fails the whole query.
	transport.queryConnectedErr = errors.New("peer gone")
	if _, err := svc.QueryClientConnected(context.Background(), users); err == nil {
		t.Fatalf("no error from failed chunk")
	}

	// No users, no requests.
	transport.connectedQueries = nil
	if connected, err := svc.QueryClientConnected(context.Background(), nil); err != nil || len(connected) != 0 {
		t.Fatalf("empty query returned %v, %v", connected, err)
	}
	if len(transport.connectedQueries) != 0 {
		t.Fatalf("empty query issued %d requests, want 0", len(transport.connectedQueries))
	}
}

func TestServiceQueryClientConnectedSingleServer(t *testing.T) {
	svc := &Service{log: dex.Disabled, transport: newSingleServerTransport()}
	connected, err := svc.QueryClientConnected(context.Background(), []account.AccountID{{0x01}, {0x02}})
	if err != nil {
		t.Fatalf("single-server QueryClientConnected error: %v", err)
	}
	if len(connected) != 0 {
		t.Fatalf("single-server connected = %v, want empty", connected)
	}
}

func TestServiceProxyClientMessageSingleServer(t *testing.T) {
	svc := &Service{log: dex.Disabled, transport: newSingleServerTransport()}
	note, err := msgjson.NewNotification(msgjson.NotifyRoute, "note")
	if err != nil {
		t.Fatalf("NewNotification error: %v", err)
	}
	if err := svc.ProxyClientMessage(context.Background(), &ClientProxyMessage{Msg: note, Broadcast: true}); err != nil {
		t.Fatalf("single-server broadcast error: %v", err)
	}
	if err := svc.ProxyClientMessage(context.Background(), &ClientProxyMessage{Msg: note}); !errors.Is(err, ErrClientProxyUnavailable) {
		t.Fatalf("single-server unicast error = %v, want %v", err, ErrClientProxyUnavailable)
	}
}

func TestEnsureLoaded(t *testing.T) {
	t.Run("order and once", func(t *testing.T) {
		var order []string
		release := make(chan struct{})
		s := &Service{
			log:       dex.Disabled,
			loaded:    newReadiness(),
			transport: newSingleServerTransport(),
			stateLoaders: []StateLoader{
				{Name: "a", Load: func(context.Context) error {
					<-release
					order = append(order, "a")
					return nil
				}},
				{Name: "b", Load: func(context.Context) error {
					order = append(order, "b")
					return nil
				}},
			},
		}

		first := make(chan error, 1)
		go func() { first <- s.ensureLoaded(context.Background()) }()

		// A second caller must wait for the first run, not skip it.
		second := make(chan error, 1)
		go func() { second <- s.ensureLoaded(context.Background()) }()

		select {
		case err := <-second:
			t.Fatalf("second caller returned before loaders ran: %v", err)
		case <-time.After(20 * time.Millisecond):
		}

		close(release)
		for i, ch := range []chan error{first, second} {
			select {
			case err := <-ch:
				if err != nil {
					t.Fatalf("caller %d: %v", i, err)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("caller %d never returned", i)
			}
		}
		if len(order) != 2 || order[0] != "a" || order[1] != "b" {
			t.Fatalf("loader order = %v, want [a b]", order)
		}
	})

	t.Run("failure resolves latch with error", func(t *testing.T) {
		boom := errors.New("boom")
		s := &Service{
			log:       dex.Disabled,
			loaded:    newReadiness(),
			transport: newSingleServerTransport(),
			stateLoaders: []StateLoader{
				{Name: "bad", Load: func(context.Context) error { return boom }},
				{Name: "never", Load: func(context.Context) error {
					t.Error("loader after a failure ran")
					return nil
				}},
			},
		}
		if err := s.ensureLoaded(context.Background()); !errors.Is(err, boom) {
			t.Fatalf("err = %v, want %v", err, boom)
		}
		if err, _ := s.loaded.result(); !errors.Is(err, boom) {
			t.Fatalf("latch = %v, want %v", err, boom)
		}
	})
}

// seedOrderTransport records the order of startup lifecycle calls, riding
// the single-server transport for everything else.
type seedOrderTransport struct {
	*singleServerTransport
	mtx   sync.Mutex
	order []string
}

func (tr *seedOrderTransport) record(step string) {
	tr.mtx.Lock()
	tr.order = append(tr.order, step)
	tr.mtx.Unlock()
}

func (tr *seedOrderTransport) ensureSeeded(context.Context) error {
	tr.record("seed")
	return nil
}

func (tr *seedOrderTransport) notifyReadyForEvents() {
	tr.record("ready-for-events")
}

func TestRunSeedsBeforeLoaders(t *testing.T) {
	tr := &seedOrderTransport{singleServerTransport: newSingleServerTransport()}
	s := &Service{
		log:       dex.Disabled,
		loaded:    newReadiness(),
		ready:     newReadiness(),
		transport: tr,
		stateLoaders: []StateLoader{{Name: "loader", Load: func(context.Context) error {
			tr.record("load")
			return nil
		}}},
	}
	tr.becameMaster = func() { s.workers.startWorkers() }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := runServiceForTest(ctx, s)

	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer waitCancel()
	if err := s.WaitUntilReadyForComms(waitCtx); err != nil {
		t.Fatalf("WaitUntilReadyForComms: %v", err)
	}
	cancel()
	<-runDone

	tr.mtx.Lock()
	order := append([]string(nil), tr.order...)
	tr.mtx.Unlock()
	want := []string{"seed", "load", "ready-for-events"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("startup order = %v, want %v", order, want)
	}
}
