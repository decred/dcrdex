//go:build live

package noderelay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/dex"
)

func TestNexus(t *testing.T) {
	netAddr, _ := net.ResolveTCPAddr("tcp", "localhost:0")
	l, _ := net.ListenTCP("tcp", netAddr)
	addr := l.Addr().String()
	l.Close()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("error splitting host and port from address %q", addr)
	}

	dir, err := os.MkdirTemp("", "")
	if err != nil {
		t.Fatalf("Error making temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	relayID := "0xabcanything_you-want"

	cfg := &NexusConfig{
		Port:     port,
		Dir:      dir,
		Key:      filepath.Join(dir, "t.key"),
		Cert:     filepath.Join(dir, "t.cert"),
		Logger:   dex.StdOutLogger("T", dex.LevelDebug),
		RelayIDs: []string{relayID},
	}

	n, err := NewNexus(cfg)
	if err != nil {
		t.Fatalf("NewNexus error: %v", err)
	}

	certB, err := os.ReadFile(cfg.Cert)
	if err != nil {
		t.Fatalf("Error reading certificate file: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	if _, err = n.Connect(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	time.Sleep(2 * time.Second)

	relayAddr, err := n.RelayAddr(relayID)
	if err != nil {
		t.Fatalf("RelayAddr error: %v", err)
	}

	firstRequest, firstResponse := "firstRequest", "firstResponse"
	var rawHandlerErr error

	var cl comms.WsConn
	cl, err = comms.NewWsConn(&comms.WsCfg{
		URL:      "wss://" + addr,
		PingWait: 20 * time.Second,
		Cert:     certB,
		ReconnectSync: func() {
			cancel()
		},
		ConnectEventFunc: func(s comms.ConnectionStatus) {},
		Logger:           dex.StdOutLogger("CL", dex.LevelDebug),
		RawHandler: func(b []byte) {
			var req *RelayedMessage
			if err := json.Unmarshal(b, &req); err != nil {
				t.Fatalf("Error unmarshaling raw message: %v", err)
			}
			switch req.MessageID {
			case 1:
				if string(req.Body) != firstRequest {
					rawHandlerErr = fmt.Errorf("first request had unexpected body %s != %s", string(req.Body), firstRequest)
					cancel()
					return
				}
				msgB, _ := json.Marshal(&RelayedMessage{
					MessageID: 1,
					Body:      []byte(firstResponse),
				})
				if err := cl.SendRaw(msgB); err != nil {
					rawHandlerErr = fmt.Errorf("Send error: %w", err)
					cancel()
				}
			}
		},
	})
	if err != nil {
		t.Fatalf("NewWsConn error: %v", err)
	}

	cm := dex.NewConnectionMaster(cl)
	if err := cm.ConnectOnce(ctx); err != nil {
		t.Fatalf("ConnectOnce error: %v", err)
	}

	msgB, _ := json.Marshal(&RelayedMessage{
		MessageID: 0, // 0 is always the node ID
		Body:      []byte(relayID),
	})

	cl.SendRaw(msgB)

	select {
	case <-n.WaitForSourceNodes():
	case <-ctx.Done():
		return
	case <-time.After(time.Second * 5):
		t.Fatal("timed out waiting for source nodes to connect")
	}

	// Now act like an asset backend and send a request through the relay node.
	relayURL := "http://" + relayAddr
	resp, err := http.DefaultClient.Post(relayURL, "application/json", strings.NewReader(firstRequest))
	if err != nil {
		t.Fatalf("Post error: %v", err)
	}

	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("First response read error: %v", err)
	}
	respBody := strings.TrimSpace(string(b))
	if respBody != firstResponse {
		t.Fatalf("wrong first response. %s != %s", string(b), firstResponse)
	}
	cancel()
	cm.Wait()
	n.wg.Wait()

	if rawHandlerErr != nil {
		t.Fatalf("Error generated in node source request handling: %v", rawHandlerErr)
	}
	fmt.Println("!!!!! Success !!!!!")
}
