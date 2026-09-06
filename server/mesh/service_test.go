// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
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

func (f *testTransport) canExecuteCommandLocally() bool {
	return f.master
}

func (f *testTransport) canForwardCommand() bool {
	return f.slave
}

func (f *testTransport) checkEventPublishAvailable(bool) error {
	return f.eventPublishErr
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

func (s *testCommandResponder) requireNoResponses(t *testing.T) {
	t.Helper()
	if len(s.sent) != 0 {
		t.Fatalf("responses = %d, want 0", len(s.sent))
	}
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
	tests := []struct {
		name                string
		transport           *testTransport
		mutateReq           func(*CommandRequest)
		wantErrCode         int
		wantExecutedLocally bool
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
			name:        "unavailable mesh rejects retryably",
			transport:   &testTransport{},
			wantErrCode: msgjson.TryAgainLaterError,
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
			requireExecuteCommandError(t, rpcErr, tt.wantErrCode, nil)

			if tt.wantExecutedLocally {
				svc.requireExecutedCommand(t, req)
				svc.requireAppliedEvents(t, 1)
				responder.requireResult(t, result)
			} else {
				svc.requireNoExecutedCommands(t)
				svc.requireNoAppliedEvents(t)
				responder.requireNoResponses(t)
			}
		})
	}
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
