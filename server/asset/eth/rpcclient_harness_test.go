//go:build harness && lgpl
// +build harness,lgpl

// This test requires that the testnet harness be running and the unix socket
// be located at $HOME/dextest/eth/alpha/node/geth.ipc

package eth

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"

	"context"
	"testing"

	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

var (
	homeDir          = os.Getenv("HOME")
	ipc              = filepath.Join(homeDir, "dextest/eth/alpha/node/geth.ipc")
	contractAddrFile = filepath.Join(homeDir, "dextest", "eth", "eth_swap_contract_address.txt")
	alphaAddress     = "18d65fb8d60c1199bb1ad381be47aa692b482605"
	gammaAddress     = "41293c2032bac60aa747374e966f79f575d42379"
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

func TestBestHeader(t *testing.T) {
	_, err := ethClient.bestHeader(ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func TestHeaderByHeight(t *testing.T) {
	_, err := ethClient.headerByHeight(ctx, 0)
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

func TestSyncProgress(t *testing.T) {
	_, err := ethClient.syncProgress(ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSuggestGasTipCap(t *testing.T) {
	_, err := ethClient.suggestGasTipCap(ctx)
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

func TestAccountBalance(t *testing.T) {
	addr := common.HexToAddress(alphaAddress)
	const vGwei = 1e7

	balBefore, err := ethClient.accountBalance(ctx, addr)
	if err != nil {
		t.Fatalf("accountBalance error: %v", err)
	}

	if err := tmuxSend(alphaAddress, gammaAddress, vGwei); err != nil {
		t.Fatalf("send error: %v", err)
	}

	balAfter, err := ethClient.accountBalance(ctx, addr)
	if err != nil {
		t.Fatalf("accountBalance error: %v", err)
	}

	diff := new(big.Int).Sub(balBefore, balAfter)
	if diff.Cmp(dexeth.GweiToWei(vGwei)) <= 0 {
		t.Fatalf("account balance changed by %d. expected > %d", dexeth.WeiToGwei(diff), uint64(vGwei))
	}
}

func tmuxRun(cmd string) error {
	cmd += "; tmux wait-for -S harnessdone"
	err := exec.Command("tmux", "send-keys", "-t", "eth-harness:0", cmd, "C-m").Run() // ; wait-for harnessdone
	if err != nil {
		return nil
	}
	return exec.Command("tmux", "wait-for", "harnessdone").Run()
}

func tmuxSend(from, to string, v uint64) error {
	return tmuxRun(fmt.Sprintf("./alpha attach --preload send.js --exec \"send(\\\"%s\\\",\\\"%s\\\",%s)\"", from, to, dexeth.GweiToWei(v)))
}
