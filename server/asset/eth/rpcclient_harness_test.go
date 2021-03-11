// +build harness
//
// This test requires that the testnet harness be running and the unix socket
// be located at $HOME/dextest/eth/alpha/node/geth.ipc

package eth

import (
	"fmt"
	"os"
	"path/filepath"

	"context"
	"testing"
)

var (
	homeDir   = os.Getenv("HOME")
	ipc       = filepath.Join(homeDir, "dextest/eth/alpha/node/geth.ipc")
	ethClient = new(rpcclient)
	ctx       context.Context
)

func TestMain(m *testing.M) {
	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer func() {
		cancel()
		ethClient.shutdown()
	}()
	if err := ethClient.connect(ctx, ipc); err != nil {
		fmt.Printf("Connect error: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestBestBlockHash(t *testing.T) {
	_, err := ethClient.bestBlockHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBestHeader(t *testing.T) {
	_, err := ethClient.bestHeader(ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBlock(t *testing.T) {
	h, err := ethClient.bestBlockHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ethClient.block(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
}
