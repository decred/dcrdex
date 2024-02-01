//go:build live

package tor

import (
	"context"
	"fmt"
	"os"
	"testing"

	"decred.org/dcrdex/dex"
)

func TestConnect(t *testing.T) {
	dataDir, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(dataDir)

	log := dex.StdOutLogger("T", dex.LevelDebug)
	relay, err := New(dataDir, log)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	cm := dex.NewConnectionMaster(relay)
	defer cm.Wait()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cm.ConnectOnce(ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	fmt.Println("Generated server address:", relay.ServerAddress())
	fmt.Println("Generated onion address:", relay.OnionAddress())
}
