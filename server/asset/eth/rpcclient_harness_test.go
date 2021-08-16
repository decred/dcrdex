//go:build harness
// +build harness

// This test requires that the testnet harness be running and the unix socket
// be located at $HOME/dextest/eth/alpha/node/geth.ipc

package eth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"decred.org/dcrdex/dex"
)

var (
	homeDir   = os.Getenv("HOME")
	cfgFile   = filepath.Join(homeDir, "dextest", "eth", "alpha", "alpha.conf")
	ethClient = new(rpcclient)
	ctx       context.Context
)

func TestMain(m *testing.M) {
	run := func() (int, error) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		defer func() {
			cancel()
			ethClient.shutdown()
		}()

		cfg, err := loadConfig(cfgFile, dex.Simnet)
		if err != nil {
			return 1, fmt.Errorf("loadConfig error: %v\n", err)
		}

		if err := ethClient.connect(ctx, cfg); err != nil {
			return 1, fmt.Errorf("Connect error: %v\n", err)
		}
		return m.Run(), nil
	}

	exitCode, err := run()
	if err != nil {
		fmt.Println(err)
	}

	os.Exit(exitCode)
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

func TestBlockNumber(t *testing.T) {
	_, err := ethClient.blockNumber(ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPeers(t *testing.T) {
	_, err := ethClient.peers(ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSyncProgress(t *testing.T) {
	_, err := ethClient.syncProgress(ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSuggestGasPrice(t *testing.T) {
	_, err := ethClient.suggestGasPrice(ctx)
	if err != nil {
		t.Fatal(err)
	}
}
