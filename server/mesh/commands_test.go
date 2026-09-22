// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/server/account"
)

type testCommandResponder struct {
	sent []*msgjson.Message
	err  error
}

func (s *testCommandResponder) Send(msg *msgjson.Message) error {
	s.sent = append(s.sent, msg)
	return s.err
}

func mustMarshalJSON(t testing.TB, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	return b
}

func requireJSONResult(t testing.TB, raw json.RawMessage, want any) {
	t.Helper()
	if want == nil {
		var got any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("result decode into nil: %v", err)
		}
		if got != nil {
			t.Fatalf("result = %v, want nil", got)
		}
		return
	}

	wantType := reflect.TypeOf(want)
	gotPtr := reflect.New(wantType)
	if err := json.Unmarshal(raw, gotPtr.Interface()); err != nil {
		t.Fatalf("result decode into %T: %v", want, err)
	}
	got := gotPtr.Elem().Interface()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("result = %#v, want %#v", got, want)
	}
}

func (s *testCommandResponder) requireResult(t testing.TB, want any) {
	t.Helper()
	if len(s.sent) != 1 {
		t.Fatalf("responses = %d, want 1", len(s.sent))
	}
	resp, err := s.sent[0].Response()
	if err != nil {
		t.Fatalf("response decode: %v", err)
	}
	requireJSONResult(t, resp.Result, want)
}

func testCommandRequest(t *testing.T) CommandRequest {
	t.Helper()
	msg, err := msgjson.NewRequest(42, "test_route", map[string]string{"ok": "true"})
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}
	return CommandRequest{
		Kind:    "test",
		User:    account.AccountID{0x01},
		Msg:     msg,
		Respond: func(*msgjson.Message) error { return nil },
	}
}

func newTestCommandCoordinator(transport meshTransport, applyEvent func(context.Context, *Event, eventOrigin) (any, error)) *commandCoordinator {
	var ct commandTransport = newSingleServerTransport()
	if transport != nil {
		ct = transport
	}
	return newCommandCoordinator(dex.Disabled, ct, "test-node", nil, applyEvent)
}

func requireNoPendingCommand(t testing.TB, commands *commandCoordinator, commandID string) {
	t.Helper()
	commands.pendingMtx.Lock()
	defer commands.pendingMtx.Unlock()
	if commands.pending[commandID] != nil {
		t.Fatalf("pending command %q was not removed", commandID)
	}
}

func TestCommandCoordinatorExecuteForwarded(t *testing.T) {
	req := testCommandRequest(t)
	responder := new(testCommandResponder)
	req.Respond = responder.Send
	result := map[string]string{"status": "ok"}
	transport := new(testTransport)
	var got *CommandContext

	commands := newCommandCoordinator(dex.Disabled, transport, "test-node", map[string]CommandExecutor{
		"test": func(cmd *CommandContext) *msgjson.Error {
			got = cmd
			if err := cmd.Completion.Complete(cmd.Context, result); err != nil {
				return msgjson.NewError(msgjson.RPCInternalError, "complete failed: %v", err)
			}
			return nil
		},
	}, nil)

	if msgErr := commands.executeForwarded(context.Background(), "cmd-forwarded", req); msgErr != nil {
		t.Fatalf("executeForwarded error: %v", msgErr)
	}
	if got == nil {
		t.Fatalf("executor was not called")
	}
	if got.Request.Kind != req.Kind || got.Request.User != req.User || got.Request.Msg != req.Msg {
		t.Fatalf("executed request mismatch: %+v", got.Request)
	}
	if got.Completion.originCommandID != "cmd-forwarded" {
		t.Fatalf("completion origin command id = %q, want cmd-forwarded", got.Completion.originCommandID)
	}
	if len(responder.sent) != 0 {
		t.Fatalf("local responses = %d, want 0", len(responder.sent))
	}
	transport.requireCommandResult(t, "cmd-forwarded")
	requireJSONResult(t, transport.commandResults[0].Result, result)
}

func TestCommandCoordinatorForwardOutcomeUnknown(t *testing.T) {
	req := testCommandRequest(t)
	responder := new(testCommandResponder)
	req.Respond = responder.Send
	transport := &testTransport{slave: true, err: errors.New("boom")}
	commands := newTestCommandCoordinator(transport, nil)

	if msgErr := commands.execute(context.Background(), req); msgErr != nil {
		t.Fatalf("execute error: %v", msgErr)
	}
	responder.requireNoResponses(t)
	commandID := transport.requireForwardedCommand(t, req).CommandID

	commands.pendingMtx.Lock()
	pending := commands.pending[commandID]
	commands.pendingMtx.Unlock()
	if pending == nil {
		t.Fatal("pending entry was not held on an unknown forward outcome")
	}
	if pending.acked {
		t.Fatal("unknown forward outcome marked the pending entry acked")
	}

	result := map[string]string{"status": "ok"}
	commands.receiveForwardedResult(commandID, mustMarshalJSON(t, result))
	responder.requireResult(t, result)
	requireNoPendingCommand(t, commands, commandID)
}

func TestCommandCoordinatorForwardAckMarksPending(t *testing.T) {
	req := testCommandRequest(t)
	transport := &testTransport{slave: true}
	commands := newTestCommandCoordinator(transport, nil)

	if msgErr := commands.execute(context.Background(), req); msgErr != nil {
		t.Fatalf("execute error: %v", msgErr)
	}
	commandID := transport.requireForwardedCommand(t, req).CommandID
	defer commands.removePending(commandID)

	commands.pendingMtx.Lock()
	pending := commands.pending[commandID]
	commands.pendingMtx.Unlock()
	if pending == nil {
		t.Fatal("acked forward removed the pending entry")
	}
	if !pending.acked {
		t.Fatal("acked forward did not mark the pending entry")
	}
}

func TestCommandCoordinatorExpirePending(t *testing.T) {
	for _, acked := range []bool{false, true} {
		req := testCommandRequest(t)
		responder := new(testCommandResponder)
		req.Respond = responder.Send
		commands := newTestCommandCoordinator(nil, nil)
		commands.registerPending("cmd-exp", req)
		if acked {
			commands.markPendingAcked("cmd-exp")
		}

		commands.expirePending("cmd-exp")
		responder.requireErrorCode(t, msgjson.ResultUnavailableError)
		requireNoPendingCommand(t, commands, "cmd-exp")

		commands.expirePending("cmd-exp")
		if len(responder.sent) != 1 {
			t.Fatalf("acked=%v: responses = %d, want 1", acked, len(responder.sent))
		}
	}
}

func TestCommandCoordinatorReceiveForwardedResult(t *testing.T) {
	req := testCommandRequest(t)
	responder := new(testCommandResponder)
	req.Respond = responder.Send
	commands := newTestCommandCoordinator(nil, nil)
	commands.registerPending("cmd-ok", req)
	defer commands.removePending("cmd-ok")
	result := map[string]string{"status": "ok"}

	commands.receiveForwardedResult("cmd-ok", mustMarshalJSON(t, result))
	responder.requireResult(t, result)
	requireNoPendingCommand(t, commands, "cmd-ok")
}

func TestCommandCoordinatorReceiveForwardedFailure(t *testing.T) {
	req := testCommandRequest(t)
	responder := new(testCommandResponder)
	req.Respond = responder.Send
	commands := newTestCommandCoordinator(nil, nil)
	commands.registerPending("cmd-fail", req)
	defer commands.removePending("cmd-fail")

	commands.receiveForwardedFailure("cmd-fail", msgjson.NewError(msgjson.FundingError, "funding failed"))
	responder.requireErrorCode(t, msgjson.FundingError)
	requireNoPendingCommand(t, commands, "cmd-fail")
}

func TestCommandCoordinatorFailAllPending(t *testing.T) {
	commands := newTestCommandCoordinator(nil, nil)
	req := testCommandRequest(t)
	responder := new(testCommandResponder)
	req.Respond = responder.Send
	commands.registerPending("cmd-a", req)

	commands.failAllPending("mesh slave lost its master connection")
	responder.requireErrorCode(t, msgjson.ResultUnavailableError)
	requireNoPendingCommand(t, commands, "cmd-a")

	// Second call with nothing pending does not re-answer.
	commands.failAllPending("node promoted to mesh master")
	if len(responder.sent) != 1 {
		t.Fatalf("responses = %d, want 1", len(responder.sent))
	}
}

func TestCommandCompletionEmit(t *testing.T) {
	t.Run("passes local command origin to apply function", func(t *testing.T) {
		req := testCommandRequest(t)
		event := &Event{Kind: "test"}
		result := "ok"
		var (
			gotEvent  *Event
			gotOrigin eventOrigin
		)
		commands := newTestCommandCoordinator(nil, func(_ context.Context, event *Event, origin eventOrigin) (any, error) {
			gotEvent = event
			gotOrigin = origin
			return nil, nil
		})
		completion := newCommandCompletion("", req, commands)

		if err := completion.Emit(context.Background(), event, func() any { return result }); err != nil {
			t.Fatalf("Emit error: %v", err)
		}
		if gotEvent != event {
			t.Fatalf("event was not passed to apply function")
		}
		if gotOrigin.kind != originLocalCommand {
			t.Fatalf("origin kind = %d, want %d", gotOrigin.kind, originLocalCommand)
		}
		if gotOrigin.completion != completion {
			t.Fatalf("completion was not passed to apply function")
		}
		if gotOrigin.commandID != "" {
			t.Fatalf("local origin commandID = %q, want empty", gotOrigin.commandID)
		}
		if gotOrigin.resultFn == nil || gotOrigin.resultFn() != result {
			t.Fatalf("result = %v, want %v", gotOrigin.resultFn(), result)
		}
	})

	t.Run("passes forwarded command origin without completion", func(t *testing.T) {
		req := testCommandRequest(t)
		event := &Event{Kind: "test"}
		result := "ok"
		var gotOrigin eventOrigin
		commands := newTestCommandCoordinator(nil, func(_ context.Context, _ *Event, origin eventOrigin) (any, error) {
			gotOrigin = origin
			return nil, nil
		})
		completion := newCommandCompletion("cmd-forwarded", req, commands)

		if err := completion.Emit(context.Background(), event, func() any { return result }); err != nil {
			t.Fatalf("Emit error: %v", err)
		}
		if gotOrigin.kind != originForwardedCommand {
			t.Fatalf("origin kind = %d, want %d", gotOrigin.kind, originForwardedCommand)
		}
		if gotOrigin.commandID != "cmd-forwarded" {
			t.Fatalf("commandID = %q, want cmd-forwarded", gotOrigin.commandID)
		}
		if gotOrigin.completion != nil {
			t.Fatalf("forwarded origin unexpectedly carried completion")
		}
		if gotOrigin.resultFn == nil || gotOrigin.resultFn() != result {
			t.Fatalf("result = %v, want %v", gotOrigin.resultFn(), result)
		}
	})

	t.Run("returns apply error", func(t *testing.T) {
		req := testCommandRequest(t)
		event := &Event{Kind: "test"}
		result := "ok"
		applyErr := errors.New("apply failed")
		commands := newTestCommandCoordinator(nil, func(context.Context, *Event, eventOrigin) (any, error) {
			return nil, applyErr
		})
		completion := newCommandCompletion("", req, commands)

		err := completion.Emit(context.Background(), event, func() any { return result })
		if !errors.Is(err, applyErr) {
			t.Fatalf("Emit apply error = %v, want %v", err, applyErr)
		}
	})
}

func TestCommandCompletionFail(t *testing.T) {
	newMsg := func(t *testing.T) *msgjson.Message {
		t.Helper()
		msg, err := msgjson.NewRequest(42, "test_route", nil)
		if err != nil {
			t.Fatalf("NewRequest error: %v", err)
		}
		return msg
	}

	t.Run("local command delivers error response directly", func(t *testing.T) {
		responder := new(testCommandResponder)
		msgErr := msgjson.NewError(msgjson.FundingError, "funding failed")
		local := newCommandCompletion("", CommandRequest{
			Msg:     newMsg(t),
			Respond: responder.Send,
		}, newTestCommandCoordinator(nil, nil))

		if err := local.Fail(context.Background(), msgErr); err != nil {
			t.Fatalf("local Fail error: %v", err)
		}
		if len(responder.sent) != 1 {
			t.Fatalf("local error responses = %d, want 1", len(responder.sent))
		}
		resp, err := responder.sent[0].Response()
		if err != nil {
			t.Fatalf("local error response decode: %v", err)
		}
		if resp.Error == nil || resp.Error.Code != msgjson.FundingError {
			t.Fatalf("local error response = %+v, want funding error", resp.Error)
		}
	})

	t.Run("forwarded command sends command failure to slave", func(t *testing.T) {
		msgErr := msgjson.NewError(msgjson.FundingError, "funding failed")
		transport := new(testTransport)
		forwarded := newCommandCompletion("cmd-err", CommandRequest{Msg: newMsg(t)}, newTestCommandCoordinator(transport, nil))

		if err := forwarded.Fail(context.Background(), msgErr); err != nil {
			t.Fatalf("forwarded Fail error: %v", err)
		}
		transport.requireCommandFailure(t, "cmd-err", msgjson.FundingError)
		if gotErr := transport.commandFailures[0].Error; gotErr != msgErr {
			t.Fatalf("forwarded failure error = %v, want %v", gotErr, msgErr)
		}
	})
}

func TestCommandCompletionComplete(t *testing.T) {
	newMsg := func(t *testing.T) *msgjson.Message {
		t.Helper()
		msg, err := msgjson.NewRequest(42, "test_route", nil)
		if err != nil {
			t.Fatalf("NewRequest error: %v", err)
		}
		return msg
	}

	t.Run("local command delivers response directly", func(t *testing.T) {
		responder := new(testCommandResponder)
		local := newCommandCompletion("", CommandRequest{
			Msg:     newMsg(t),
			Respond: responder.Send,
		}, newTestCommandCoordinator(nil, nil))

		localResult := map[string]string{"status": "ok"}
		if err := local.Complete(context.Background(), localResult); err != nil {
			t.Fatalf("local Complete error: %v", err)
		}
		responder.requireResult(t, localResult)
	})

	t.Run("forwarded command sends command result to slave", func(t *testing.T) {
		transport := new(testTransport)
		forwarded := newCommandCompletion("cmd-ok", CommandRequest{Msg: newMsg(t)}, newTestCommandCoordinator(transport, nil))

		forwardedResult := map[string]string{"status": "already"}
		if err := forwarded.Complete(context.Background(), forwardedResult); err != nil {
			t.Fatalf("forwarded Complete error: %v", err)
		}
		transport.requireCommandResult(t, "cmd-ok")
		requireJSONResult(t, transport.commandResults[0].Result, forwardedResult)
	})

	t.Run("forwarded nil result is valid command result JSON", func(t *testing.T) {
		transport := new(testTransport)
		nilForwarded := newCommandCompletion("cmd-null", CommandRequest{Msg: newMsg(t)}, newTestCommandCoordinator(transport, nil))

		if err := nilForwarded.Complete(context.Background(), nil); err != nil {
			t.Fatalf("forwarded nil Complete error: %v", err)
		}
		transport.requireCommandResult(t, "cmd-null")
		nilResult := transport.commandResults[0].Result
		requireJSONResult(t, nilResult, nil)
		if err := validateCommandResult(&commandResult{CommandID: "cmd-null", Result: nilResult}); err != nil {
			t.Fatalf("nil command result validation error: %v", err)
		}
	})
}
