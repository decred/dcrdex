//go:build harness

// This test requires that the testnet harness be running and the unix socket
// be located at $HOME/dextest/eth/delta/node/geth.ipc

package eth

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"context"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

var (
	wsEndpoint   = "ws://localhost:38559" // beta ws port, with txpool api namespace enabled
	homeDir      = os.Getenv("HOME")
	alphaIPCFile = filepath.Join(homeDir, "dextest", "eth", "alpha", "node", "geth.ipc")

	contractAddrFile   = filepath.Join(homeDir, "dextest", "eth", "eth_swap_contract_address.txt")
	tokenSwapAddrFile  = filepath.Join(homeDir, "dextest", "eth", "erc20_swap_contract_address.txt")
	tokenErc20AddrFile = filepath.Join(homeDir, "dextest", "eth", "test_token_contract_address.txt")
	deltaAddress       = "d12ab7cf72ccf1f3882ec99ddc53cd415635c3be"
	gammaAddress       = "41293c2032bac60aa747374e966f79f575d42379"
	ethClient          *rpcclient
	ctx                context.Context
)

func TestMain(m *testing.M) {
	monitorConnectionsInterval = 3 * time.Second

	// Run in function so that defers happen before os.Exit is called.
	run := func() (int, error) {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(context.Background())
		defer cancel()
		log := dex.StdOutLogger("T", dex.LevelTrace)

		netAddrs, found := dexeth.ContractAddresses[ethContractVersion]
		if !found {
			return 1, fmt.Errorf("no contract address for eth version %d", ethContractVersion)
		}
		ethContractAddr, found := netAddrs[dex.Simnet]
		if !found {
			return 1, fmt.Errorf("no contract address for eth version %d on %s", ethContractVersion, dex.Simnet)
		}

		ethClient = newRPCClient(BipID, dex.Simnet, []endpoint{{url: wsEndpoint}, {url: alphaIPCFile}}, ethContractAddr, log)

		dexeth.ContractAddresses[0][dex.Simnet] = getContractAddrFromFile(contractAddrFile)

		netToken := dexeth.Tokens[testTokenID].NetTokens[dex.Simnet]
		netToken.Address = getContractAddrFromFile(tokenErc20AddrFile)
		netToken.SwapContracts[0].Address = getContractAddrFromFile(tokenSwapAddrFile)

		if err := ethClient.connect(ctx); err != nil {
			return 1, fmt.Errorf("Connect error: %w", err)
		}

		if err := ethClient.loadToken(ctx, testTokenID, registeredTokens[testTokenID]); err != nil {
			return 1, fmt.Errorf("loadToken error: %w", err)
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

func TestSuggestGasTipCap(t *testing.T) {
	_, err := ethClient.suggestGasTipCap(ctx)
	if err != nil {
		t.Fatal(err)
	}
}

func TestSwap(t *testing.T) {
	var secretHash [32]byte
	copy(secretHash[:], encode.RandomBytes(32))
	_, err := ethClient.swap(ctx, BipID, secretHash)
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
	t.Run("eth", func(t *testing.T) { testAccountBalance(t, BipID) })
	t.Run("token", func(t *testing.T) { testAccountBalance(t, testTokenID) })
}

func testAccountBalance(t *testing.T, assetID uint32) {
	addr := common.HexToAddress(deltaAddress)
	const vGwei = 1e7

	balBefore, err := ethClient.accountBalance(ctx, assetID, addr)
	if err != nil {
		t.Fatalf("accountBalance error: %v", err)
	}

	if assetID == BipID {
		err = tmuxSend(deltaAddress, gammaAddress, vGwei)
	} else {
		err = tmuxSendToken(gammaAddress, vGwei)
	}
	if err != nil {
		t.Fatalf("send error: %v", err)
	}

	// NOTE: this test does not mine the above sends, and as such the node or
	// provider for this test must have the txpool api namespace enabled.
	balAfter, err := ethClient.accountBalance(ctx, assetID, addr)
	if err != nil {
		t.Fatalf("accountBalance error: %v", err)
	}

	if assetID == BipID {
		diff := new(big.Int).Sub(balBefore, balAfter)
		if diff.Cmp(dexeth.GweiToWei(vGwei)) <= 0 {
			t.Fatalf("account balance changed by %d. expected > %d", dexeth.WeiToGwei(diff), uint64(vGwei))
		}
	}

	if assetID == testTokenID {
		diff := new(big.Int).Sub(balBefore, balAfter)
		if diff.Cmp(dexeth.GweiToWei(vGwei)) != 0 {
			t.Fatalf("account balance changed by %d. expected > %d", dexeth.WeiToGwei(diff), uint64(vGwei))
		}
	}
}

func TestMonitorHealth(t *testing.T) {
	// Requesting a non-existent transaction should propagate the error. Also
	// check logs to ensure the endpoint index was not advanced.
	_, _, err := ethClient.transaction(ctx, common.Hash{})
	if !errors.Is(err, ethereum.NotFound) {
		t.Fatalf("'not found' error not propagated. got err = %v", err)
	}
	ethClient.log.Info("Not found error successfully propagated")

	originalClients := ethClient.clientsCopy()
	originalClients[0].Close()

	fmt.Println("Waiting for client health check...")
	time.Sleep(5 * time.Second)

	updatedClients := ethClient.clientsCopy()

	fmt.Println("Original clients:", originalClients)
	fmt.Println("Updated clients:", updatedClients)

	if originalClients[0].endpoint != updatedClients[len(updatedClients)-1].endpoint {
		t.Fatalf("failing client was not moved to the end. got %s, expected %s", updatedClients[len(updatedClients)-1].endpoint, originalClients[0].endpoint)
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
	return tmuxRun(fmt.Sprintf("./delta attach --preload send.js --exec \"send(\\\"%s\\\",\\\"%s\\\",%s)\"", from, to, dexeth.GweiToWei(v)))
}

func tmuxSendToken(to string, v uint64) error {
	return tmuxRun(fmt.Sprintf("./delta attach --preload loadTestToken.js --exec \"testToken.transfer(\\\"0x%s\\\",%s)\"", to, dexeth.GweiToWei(v)))
}

func getContractAddrFromFile(fileName string) common.Address {
	addrBytes, err := os.ReadFile(fileName)
	if err != nil {
		panic(fmt.Sprintf("error reading contract address: %v", err))
	}
	addrLen := len(addrBytes)
	if addrLen == 0 {
		panic(fmt.Sprintf("no contract address found at %v", fileName))
	}
	addrStr := string(addrBytes[:addrLen-1])
	address := common.HexToAddress(addrStr)
	return address
}
