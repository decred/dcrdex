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
}
