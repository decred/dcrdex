//go:build rpclive

package eth

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/params"
)

const (
	deltaHTTPPort = "38556"
	deltaWSPort   = "38557"
)

var (
	dextestDir = filepath.Join(os.Getenv("HOME"), "dextest")
	harnessDir = filepath.Join(dextestDir, "eth", "harness-ctl")
	ctx        = context.Background()
)

func harnessCmd(ctx context.Context, exe string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = harnessDir
	op, err := cmd.CombinedOutput()
	return string(op), err
}

func mine(ctx context.Context) error {
	_, err := harnessCmd(ctx, "./mine-alpha", "1")
	return err
}

func testEndpoint(endpoints []string, syncBlocks uint64, tFunc func(context.Context, *multiRPCClient)) error {
	dir, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(dir)

	cl, err := tRPCClient(dir, encode.RandomBytes(32), endpoints, dex.Simnet, false)
	if err != nil {
		return err
	}

	fmt.Println("######## Address:", cl.creds.addr)

	if err := cl.connect(ctx); err != nil {
		return fmt.Errorf("connect error: %v", err)
	}

	startHdr, err := cl.bestHeader(ctx)
	if err != nil {
		return fmt.Errorf("error getting initial header: %v", err)
	}

	// mine headers
	start := time.Now()
	for {
		mine(ctx)
		hdr, err := cl.bestHeader(ctx)
		if err != nil {
			return fmt.Errorf("error getting best header: %v", err)
		}
		blocksMined := hdr.Number.Uint64() - startHdr.Number.Uint64()
		if blocksMined > syncBlocks {
			break
		}
		if time.Since(start) > time.Minute {
			return fmt.Errorf("timed out")
		}
		select {
		case <-time.After(time.Second * 5):
			// mine and check again
		case <-ctx.Done():
			return context.Canceled
		}
	}

	if tFunc != nil {
		tFunc(ctx, cl)
	}

	time.Sleep(time.Second)

	return nil
}

func TestHTTP(t *testing.T) {
	if err := testEndpoint([]string{"http://localhost:" + deltaHTTPPort}, 2, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWS(t *testing.T) {
	if err := testEndpoint([]string{"ws://localhost:" + deltaWSPort}, 2, nil); err != nil {
		t.Fatal(err)
	}
}

func TestWSTxLogs(t *testing.T) {
	if err := testEndpoint([]string{"ws://localhost:" + deltaWSPort}, 2, func(ctx context.Context, cl *multiRPCClient) {
		for i := 0; i < 3; i++ {
			time.Sleep(time.Second)
			harnessCmd(ctx, "./sendtoaddress", cl.creds.addr.String(), "1")
			mine(ctx)

		}
		bal, _ := cl.addressBalance(ctx, cl.creds.addr)
		fmt.Println("Balance after", bal)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSimnetMultiRPCClient(t *testing.T) {
	endpoints := []string{
		"ws://localhost:" + deltaWSPort,
		"http://localhost:" + deltaHTTPPort,
	}

	nonceProviderStickiness = time.Second / 2

	rand.Seed(time.Now().UnixNano())

	if err := testEndpoint(endpoints, 2, func(ctx context.Context, cl *multiRPCClient) {
		// Get some funds
		harnessCmd(ctx, "./sendtoaddress", cl.creds.addr.String(), "3")
		time.Sleep(time.Second)
		mine(ctx)
		time.Sleep(time.Second)

		if err := cl.unlock("abc"); err != nil {
			t.Fatalf("error unlocking: %v", err)
		}

		const amt = 1e8 // 0.1 ETH
		var alphaAddr = common.HexToAddress("0x18d65fb8d60c1199bb1ad381be47aa692b482605")

		for i := 0; i < 10; i++ {
			txOpts, err := cl.txOpts(ctx, amt, defaultSendGasLimit, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			// Send two in a row. They should use each provider, preferred first.
			for j := 0; j < 2; j++ {
				if _, err := cl.sendTransaction(ctx, txOpts, alphaAddr, nil); err != nil {
					t.Fatalf("error sending tx %d-%d: %v", i, j, err)
				}
			}
			_, err = cl.bestHeader(ctx)
			// Let nonce provider expire. The pair should use the same
			// provider, but different pairs can use different providers. Look
			// for the variation.
			mine(ctx)
			time.Sleep(time.Second)
		}
	}); err != nil {
		t.Fatal(err)
	}
}

//
// Create a providers.json file in your .dexc directory.
//   1. Seed can be anything. Just generate randomness.
//   2. Can connect to a host's websocket and http endpoints simultaneously.
//      Actually nothing preventing you from connecting to a single provider
//      100 times, but that may be a guardrail added in the future.
//
// Example ~/.dexc/providers.json
/*
{
    "testnet": {
        "seed": "9e0084387c3ba7ac4b5bb409c220c08d4ee74f7b8c73b03fff18c727c5ce9f47",
        "providers": [
            "https://goerli.infura.io/v3/<API KEY>",
            "https://<API KEY>.eth.rpc.rivet.cloud",
            "https://eth-goerli.g.alchemy.com/v2/<API KEY>-"
        ]
    },
    "mainnet": {
        "seed": "9e0084387c3ba7ac4b5bb409c220c08d4ee74f7b8c73b03fff18c727c5ce9f47",
        "providers": [
            "wss://mainnet.infura.io/ws/v3/<API KEY>",
            "https://<API KEY>.eth.rpc.rivet.cloud",
            "https://eth-mainnet.g.alchemy.com/v2/<API KEY>"
        ]
    }
}
*/

type tProvider struct {
	Seed      dex.Bytes `json:"seed"`
	Providers []string  `json:"providers"`
}

func readProviderFile(t *testing.T, net dex.Network) *tProvider {
	t.Helper()
	providersFile := filepath.Join(dextestDir, "providers.json")
	b, err := os.ReadFile(providersFile)
	if err != nil {
		t.Fatalf("ReadFile(%q) error: %v", providersFile, err)
	}
	var accounts map[string]*tProvider
	if err := json.Unmarshal(b, &accounts); err != nil {
		t.Fatal(err)
	}
	accts := accounts[net.String()]
	if accts == nil {
		t.Fatalf("no")
	}
	return accts
}

func TestMonitorTestnet(t *testing.T) {
	testMonitorNet(t, dex.Testnet)
}

func TestMonitorMainnet(t *testing.T) {
	testMonitorNet(t, dex.Mainnet)
}

func testMonitorNet(t *testing.T, net dex.Network) {
	providerFile := readProviderFile(t, net)
	dir, _ := os.MkdirTemp("", "")

	cl, err := tRPCClient(dir, providerFile.Seed, providerFile.Providers, net, true)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()

	if err := cl.connect(ctx); err != nil {
		t.Fatalf("Connection error: %v", err)
	}
	<-ctx.Done()
}

func TestRPC(t *testing.T) {
	endpoint := os.Getenv("PROVIDER")
	if endpoint == "" {
		t.Fatalf("specify a provider in the PROVIDER environmental variable")
	}
	dir, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(dir)
	cl, err := tRPCClient(dir, encode.RandomBytes(32), []string{endpoint}, dex.Mainnet, true)
	if err != nil {
		t.Fatal(err)
	}

	if err := cl.connect(ctx); err != nil {
		t.Fatalf("connect error: %v", err)
	}

	for _, tt := range newCompatibilityTests(cl, big.NewInt(chainIDs[dex.Mainnet]), dex.Mainnet, cl.log) {
		tStart := time.Now()
		if err := cl.withAny(ctx, tt.f); err != nil {
			t.Fatalf("%q: %v", tt.name, err)
		}
		fmt.Printf("### %q: %s \n", tt.name, time.Since(tStart))
	}
}

var freeServers = []string{
	"https://cloudflare-eth.com/", // cloudflare-eth.com "SuggestGasTipCap" error: Method not found
	"https://main-rpc.linkpool.io/",
	"https://nodes.mewapi.io/rpc/eth",
	"https://rpc.flashbots.net/",
	"https://rpc.ankr.com/eth", // Passes, but doesn't support SyncProgress, which don't use and just lie about right now.
	"https://api.mycryptoapi.com/eth",
	"https://ethereumnodelight.app.runonflux.io",
}

func TestFreeServers(t *testing.T) {
	runTest := func(endpoint string) error {
		dir, _ := os.MkdirTemp("", "")
		defer os.RemoveAll(dir)
		cl, err := tRPCClient(dir, encode.RandomBytes(32), []string{endpoint}, dex.Mainnet, true)
		if err != nil {
			return fmt.Errorf("tRPCClient error: %v", err)
		}
		if err := cl.connect(ctx); err != nil {
			return fmt.Errorf("connect error: %v", err)
		}
		for _, tt := range newCompatibilityTests(cl, big.NewInt(chainIDs[dex.Mainnet]), dex.Mainnet, cl.log) {
			if err := cl.withAny(ctx, tt.f); err != nil {
				return fmt.Errorf("%q error: %v", tt.name, err)
			}
			fmt.Printf("#### %q passed %q \n", endpoint, tt.name)
		}
		return nil
	}

	passes, fails := make([]string, 0), make(map[string]error, 0)
	for _, endpoint := range freeServers {
		if err := runTest(endpoint); err != nil {
			fails[endpoint] = err
		} else {
			passes = append(passes, endpoint)
		}
	}
	for _, pass := range passes {
		fmt.Printf("!!!! %q PASSED \n", pass)
	}
	for endpoint, err := range fails {
		fmt.Printf("XXXX %q FAILED : %v \n", endpoint, err)
	}
}

func TestMainnetCompliance(t *testing.T) {
	providerFile := readProviderFile(t, dex.Mainnet)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log := dex.StdOutLogger("T", dex.LevelTrace)
	providers, err := connectProviders(ctx, providerFile.Providers, log, big.NewInt(chainIDs[dex.Mainnet]), dex.Mainnet)
	if err != nil {
		t.Fatal(err)
	}
	err = checkProvidersCompliance(ctx, providers, log)
	if err != nil {
		t.Fatal(err)
	}
}

func tRPCClient(dir string, seed []byte, endpoints []string, net dex.Network, skipConnect bool) (*multiRPCClient, error) {
	wDir := getWalletDir(dir, net)
	err := os.MkdirAll(wDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("os.Mkdir error: %w", err)
	}

	log := dex.StdOutLogger("T", dex.LevelTrace)

	chainID := dexeth.ChainIDs[net]
	if err := CreateEVMWallet(chainID, &asset.CreateWalletParams{
		Type: walletTypeRPC,
		Seed: seed,
		Pass: []byte("abc"),
		Settings: map[string]string{
			"providers": strings.Join(endpoints, " "),
		},
		DataDir: dir,
		Net:     net,
		Logger:  dex.StdOutLogger("T", dex.LevelTrace),
	}, skipConnect); err != nil {
		return nil, fmt.Errorf("error creating wallet: %v", err)
	}

	var c ethconfig.Config
	switch net {
	case dex.Mainnet:
		c = ethconfig.Defaults
		c.Genesis = core.DefaultGenesisBlock()
		c.NetworkId = params.MainnetChainConfig.ChainID.Uint64()
	default:
		c, err = chainConfig(chainID, net)
		if err != nil {
			return nil, fmt.Errorf("error getting chain config: %v", err)
		}
		c.NetworkId = c.Genesis.Config.ChainID.Uint64()
	}

	return newMultiRPCClient(dir, endpoints, log, c.Genesis.Config, big.NewInt(chainID), net)
}
