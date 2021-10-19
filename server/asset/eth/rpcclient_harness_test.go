//go:build harness && lgpl
// +build harness,lgpl

// This test requires that the testnet harness be running and the unix socket
// be located at $HOME/dextest/eth/alpha/node/geth.ipc

package eth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"context"
	"testing"

	"decred.org/dcrdex/dex/encode"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

var (
	homeDir          = os.Getenv("HOME")
	ipc              = filepath.Join(homeDir, "dextest/eth/alpha/node/geth.ipc")
	contractAddrFile = filepath.Join(homeDir, "dextest", "eth", "contract_addr.txt")
	ethClient        = new(rpcclient)
	ctx              context.Context
)

func TestMain(m *testing.M) {
	// Run in function so that defers happen before os.Exit is called.
	run := func() (int, error) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		defer func() {
			cancel()
			ethClient.shutdown()
		}()
		ctx, cancel = context.WithCancel(context.Background())
		defer func() {
			cancel()
			ethClient.shutdown()
		}()
		addrBytes, err := os.ReadFile(contractAddrFile)
		if err != nil {
			return 1, fmt.Errorf("error reading contract address: %v", err)
		}
		addrLen := len(addrBytes)
		if addrLen == 0 {
			return 1, fmt.Errorf("no contract address found at %v", contractAddrFile)
		}
		addrStr := string(addrBytes[:addrLen-1])
		contractAddr := common.HexToAddress(addrStr)
		fmt.Printf("Contract address is %v\n", addrStr)
		if err := ethClient.connect(ctx, ipc, &contractAddr); err != nil {
			return 1, fmt.Errorf("Connect error: %v", err)
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

func TestSwap(t *testing.T) {
	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	_, err := ethClient.swap(ctx, secretHash)
	if err != nil {
		t.Fatal(err)
	}
}

func TestTransaction(t *testing.T) {
	var hash [32]byte
	copy(hash[:], encode.RandomBytes(32))
	_, _, err := ethClient.transaction(ctx, hash)
	// TODO: Test positive route.
	if !errors.Is(err, ethereum.NotFound) {
		t.Fatal(err)
	}
}
