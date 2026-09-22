// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package mesh

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"github.com/gorilla/websocket"
)

func TestMeshConnHandleMessageAuthGate(t *testing.T) {
	conn := &rpcConn{
		ctx: context.Background(),
		routes: map[string]meshRoute{
			helloRoute: {
				handler: func(context.Context, link, *msgjson.Message) *msgjson.Error {
					return nil
				},
			},
			helloDecisionRoute: {
				requiresAuth: true,
				handler: func(context.Context, link, *msgjson.Message) *msgjson.Error {
					t.Fatalf("decision handler should not be called on an unauthorized connection")
					return nil
				},
			},
		},
		respHandlers: make(map[uint64]chan<- *msgjson.Message),
	}

	msg, err := msgjson.NewRequest(1, helloDecisionRoute, struct{}{})
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}

	rpcErr := conn.handleMessage(msg)
	if rpcErr == nil || rpcErr.Code != msgjson.UnauthorizedConnection {
		t.Fatalf("wrong rpc error: %+v", rpcErr)
	}
}

func TestMeshConnHandleMessageAllowsHelloBeforeAuth(t *testing.T) {
	type ctxKey struct{}
	called := false
	ctx := context.WithValue(context.Background(), ctxKey{}, "route-context")
	conn := &rpcConn{
		ctx: ctx,
		routes: map[string]meshRoute{
			helloRoute: {
				handler: func(got context.Context, _ link, _ *msgjson.Message) *msgjson.Error {
					if got.Value(ctxKey{}) != "route-context" {
						t.Fatalf("route context value = %v, want route-context", got.Value(ctxKey{}))
					}
					called = true
					return nil
				},
			},
		},
		respHandlers: make(map[uint64]chan<- *msgjson.Message),
	}

	msg, err := msgjson.NewRequest(1, helloRoute, struct{}{})
	if err != nil {
		t.Fatalf("NewRequest error: %v", err)
	}

	if rpcErr := conn.handleMessage(msg); rpcErr != nil {
		t.Fatalf("handleMessage error: %+v", rpcErr)
	}
	if !called {
		t.Fatalf("hello handler was not called")
	}
}

type tWSConn struct {
	readDeadlineErr error

	closed     chan struct{}
	closeCount int
	mtx        sync.Mutex
	once       sync.Once
}

func (c *tWSConn) Close() error {
	c.mtx.Lock()
	c.closeCount++
	c.mtx.Unlock()
	c.once.Do(func() {
		close(c.closed)
	})
	return nil
}

func (c *tWSConn) SetReadLimit(int64) {}

func (c *tWSConn) SetReadDeadline(time.Time) error {
	return c.readDeadlineErr
}

func (c *tWSConn) ReadMessage() (int, []byte, error) {
	<-c.closed
	return 0, nil, &websocket.CloseError{Code: websocket.CloseNormalClosure}
}

func (c *tWSConn) SetWriteDeadline(time.Time) error {
	return nil
}

func (c *tWSConn) WriteMessage(int, []byte) error {
	return nil
}

func (c *tWSConn) WriteControl(int, []byte, time.Time) error {
	return nil
}

func (c *tWSConn) closes() int {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	return c.closeCount
}

func TestMeshConnConnectLifecycle(t *testing.T) {
	t.Run("disconnect cancels route context", func(t *testing.T) {
		wsConn := &tWSConn{closed: make(chan struct{})}
		conn := newRPCConn(t.Context(), "test-peer", wsConn, nil, dex.Disabled)

		wg, err := conn.connect()
		if err != nil {
			t.Fatalf("connect error: %v", err)
		}

		conn.Disconnect()
		defer wg.Wait()

		select {
		case <-conn.ctx.Done():
		case <-time.After(time.Second):
			t.Fatalf("route context was not canceled after disconnect")
		}
	})

	t.Run("connect failure cancels route context and closes websocket", func(t *testing.T) {
		readDeadlineErr := errors.New("set read deadline")
		wsConn := &tWSConn{
			closed:          make(chan struct{}),
			readDeadlineErr: readDeadlineErr,
		}
		conn := newRPCConn(t.Context(), "test-peer", wsConn, nil, dex.Disabled)

		wg, err := conn.connect()
		if !errors.Is(err, readDeadlineErr) {
			t.Fatalf("connect error = %v, want %v", err, readDeadlineErr)
		}
		if wg != nil {
			t.Fatalf("connect returned waitgroup on error")
		}
		if got := wsConn.closes(); got != 1 {
			t.Fatalf("websocket close count = %d, want 1", got)
		}
		select {
		case <-conn.ctx.Done():
		case <-time.After(time.Second):
			t.Fatalf("route context was not canceled after connect failure")
		}
	})
}

func TestPeerRPCError(t *testing.T) {
	err := &peerRPCError{Code: msgjson.SubscribeRejectedError, Message: "bad tip"}
	var p *peerRPCError
	if !errors.As(fmt.Errorf("wrap: %w", err), &p) || p.Code != msgjson.SubscribeRejectedError {
		t.Fatalf("errors.As peerRPCError = (%v, ok)", p)
	}
	msgErr := err.MsgError()
	if msgErr.Code != err.Code || msgErr.Message != err.Message {
		t.Fatalf("MsgError = %#v, want code %d message %q", msgErr, err.Code, err.Message)
	}
}
