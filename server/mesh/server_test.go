package mesh

import (
	"context"
	"net/url"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"github.com/gorilla/websocket"
)

func TestNewMeshServerValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *meshServerConfig
		wantErr bool
	}{
		{name: "nil cfg", cfg: nil, wantErr: true},
		{name: "empty listen addr", cfg: &meshServerConfig{NoTLS: true}, wantErr: true},
		{name: "tls without cert/key", cfg: &meshServerConfig{ListenAddr: "127.0.0.1:0"}, wantErr: true},
		{name: "happy path", cfg: &meshServerConfig{ListenAddr: "127.0.0.1:0", NoTLS: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := newMeshServer(tt.cfg, nil, dex.Disabled)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if s != nil {
				_ = s.listener.Close()
			}
		})
	}
}

func TestMeshServerLifecycle(t *testing.T) {
	s, err := newMeshServer(&meshServerConfig{
		ListenAddr: "127.0.0.1:0",
		NoTLS:      true,
	}, nil, dex.Disabled)
	if err != nil {
		t.Fatalf("newMeshServer: %v", err)
	}
	addr := s.listener.Addr().String()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- s.Run(ctx) }()

	u := url.URL{Scheme: "ws", Host: addr, Path: meshWSPath}
	client, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer client.Close()

	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run err = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}

	// The server closes the websocket with a normal close frame. A deadline
	// error here would mean the socket was left open.
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := client.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		t.Fatalf("client read after server shutdown = %v, want a normal close", err)
	}
}
