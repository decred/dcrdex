//go:build rpclive

package eth

import (
	"context"
	"flag"
	"fmt"
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
	"github.com/ethereum/go-ethereum/params"
)

type MRPCTest struct {
	ctx               context.Context
	chain             string
	chainConfigLookup func(net dex.Network) (*params.ChainConfig, error)
	compatDataLookup  func(net dex.Network) (CompatibilityData, error)
	harnessDirectory  string
	credentialsFile   string
}

// NewMRPCTest creates a new MRPCTest.
// Create a credntials.json file in your ~/dextest directory.
// See README for getgas for format
func NewMRPCTest(
	ctx context.Context,
	cfgLookup func(net dex.Network) (*params.ChainConfig, error),
	compatLookup func(net dex.Network) (c CompatibilityData, err error),
	chainSymbol string,
) *MRPCTest {

	var skipWS bool
	flag.BoolVar(&skipWS, "skipws", false, "skip attempt to automatically resolve WebSocket URL from HTTP(S) URL")
	flag.Parse()
	if skipWS {
		forceTryWS = false
	}

	dextestDir := filepath.Join(os.Getenv("HOME"), "dextest")
	return &MRPCTest{
		ctx:               ctx,
		chain:             chainSymbol,
		chainConfigLookup: cfgLookup,
		compatDataLookup:  compatLookup,
		harnessDirectory:  filepath.Join(dextestDir, chainSymbol, "harness-ctl"),
		credentialsFile:   filepath.Join(dextestDir, "credentials.json"),
	}
}

func (m *MRPCTest) rpcClient(dir string, seed []byte, endpoints []string, net dex.Network, skipConnect bool) (*multiRPCClient, error) {
	wDir := getWalletDir(dir, net)
	err := os.MkdirAll(wDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("os.Mkdir error: %w", err)
	}

	log := dex.StdOutLogger("T", dex.LevelTrace)

	cfg, err := m.chainConfigLookup(net)
	if err != nil {
		return nil, fmt.Errorf("chainConfigLookup error: %v", err)
	}

	compat, err := m.compatDataLookup(net)
	if err != nil {
		return nil, fmt.Errorf("compatDataLookup error: %v", err)
	}

	chainID := cfg.ChainID.Int64()
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
	}, &compat, skipConnect); err != nil {
		return nil, fmt.Errorf("error creating wallet: %v", err)
	}

	return newMultiRPCClient(dir, endpoints, log, cfg, net)
}

func (m *MRPCTest) TestHTTP(t *testing.T, port string) {
	if err := m.testSimnetEndpoint([]string{"http://localhost:" + port}, 2, nil); err != nil {
		t.Fatal(err)
	}
}

func (m *MRPCTest) TestWS(t *testing.T, port string) {
	if err := m.testSimnetEndpoint([]string{"ws://localhost:" + port}, 2, nil); err != nil {
		t.Fatal(err)
	}
}

func (m *MRPCTest) TestWSTxLogs(t *testing.T, port string) {
	if err := m.testSimnetEndpoint([]string{"ws://localhost:" + port}, 2, func(ctx context.Context, cl *multiRPCClient) {
		for i := 0; i < 3; i++ {
			time.Sleep(time.Second)
			m.harnessCmd(ctx, "./sendtoaddress", cl.creds.addr.String(), "1")
			m.mine(ctx)

		}
		bal, _ := cl.addressBalance(ctx, cl.creds.addr)
		fmt.Println("Balance after", bal)
	}); err != nil {
		t.Fatal(err)
	}
}

func (m *MRPCTest) TestSimnetMultiRPCClient(t *testing.T, wsPort, httpPort string) {
	endpoints := []string{
		"ws://localhost:" + wsPort,
		"http://localhost:" + httpPort,
	}

	nonceProviderStickiness = time.Second / 2

	rand.Seed(time.Now().UnixNano())

	if err := m.testSimnetEndpoint(endpoints, 2, func(ctx context.Context, cl *multiRPCClient) {
		// Get some funds
		m.harnessCmd(ctx, "./sendtoaddress", cl.creds.addr.String(), "3")

		time.Sleep(time.Second)
		m.mine(ctx)
		time.Sleep(time.Second)

		if err := cl.unlock("abc"); err != nil {
			t.Fatalf("error unlocking: %v", err)
		}

		const amt = 1e8 // 0.1 ETH
		var alphaAddr = common.HexToAddress("0x18d65fb8d60c1199bb1ad381be47aa692b482605")

		for i := 0; i < 10; i++ {
			// Send two in a row. They should use each provider, preferred first.
			for j := 0; j < 2; j++ {
				txOpts, err := cl.txOpts(ctx, amt, defaultSendGasLimit, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := cl.sendTransaction(ctx, txOpts, alphaAddr, nil); err != nil {
					t.Fatalf("error sending tx %d-%d: %v", i, j, err)
				}
			}
			_, err := cl.bestHeader(ctx)
			if err != nil {
				t.Fatalf("bestHeader error: %v", err)
			}
			// Let nonce provider expire. The pair should use the same
			// provider, but different pairs can use different providers. Look
			// for the variation.
			m.mine(ctx)
			time.Sleep(time.Second)
		}
	}); err != nil {
		t.Fatal(err)
	}
}

func (m *MRPCTest) TestMonitorNet(t *testing.T, net dex.Network) {
	seed, providers := m.readProviderFile(t, net)
	dir, _ := os.MkdirTemp("", "")

	cl, err := m.rpcClient(dir, seed, providers, net, true)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(m.ctx, time.Hour)
	defer cancel()

	if err := cl.connect(ctx); err != nil {
		t.Fatalf("Connection error: %v", err)
	}
	<-ctx.Done()
}

func (m *MRPCTest) TestRPC(t *testing.T, net dex.Network) {
	// To skip automatic websocket resolution, pass flag --skipws.

	endpoint := os.Getenv("PROVIDER")
	if endpoint == "" {
		t.Fatalf("specify a provider in the PROVIDER environmental variable")
	}
	dir, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(dir)
	cl, err := m.rpcClient(dir, encode.RandomBytes(32), []string{endpoint}, net, true)
	if err != nil {
		t.Fatal(err)
	}

	if err := cl.connect(m.ctx); err != nil {
		t.Fatalf("connect error: %v", err)
	}

	compat, err := m.compatDataLookup(net)
	if err != nil {
		t.Fatalf("compatDataLookup error: %v", err)
	}

	for _, tt := range newCompatibilityTests(cl, &compat, cl.log) {
		tStart := time.Now()
		if err := cl.withAny(m.ctx, tt.f); err != nil {
			t.Fatalf("%q: %v", tt.name, err)
		}
		fmt.Printf("### %q: %s \n", tt.name, time.Since(tStart))
	}
}

func (m *MRPCTest) TestFreeServers(t *testing.T, freeServers []string, net dex.Network) {
	compat, err := m.compatDataLookup(net)
	if err != nil {
		t.Fatalf("compatDataLookup error: %v", err)
	}
	runTest := func(endpoint string) error {
		dir, _ := os.MkdirTemp("", "")
		defer os.RemoveAll(dir)
		cl, err := m.rpcClient(dir, encode.RandomBytes(32), []string{endpoint}, net, true)
		if err != nil {
			return fmt.Errorf("tRPCClient error: %v", err)
		}
		if err := cl.connect(m.ctx); err != nil {
			return fmt.Errorf("connect error: %v", err)
		}
		for _, tt := range newCompatibilityTests(cl, &compat, cl.log) {
			if err := cl.withAny(m.ctx, tt.f); err != nil {
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

func (m *MRPCTest) TestMainnetCompliance(t *testing.T) {
	_, providerLookup := m.readProviderFile(t, dex.Mainnet)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := m.chainConfigLookup(dex.Mainnet)
	if err != nil {
		t.Fatalf("chainConfigLookup error: %v", err)
	}

	compat, err := m.compatDataLookup(dex.Mainnet)
	if err != nil {
		t.Fatalf("compatDataLookup error: %v", err)
	}

	log := dex.StdOutLogger("T", dex.LevelTrace)
	providers, err := connectProviders(ctx, providerLookup, log, cfg.ChainID, dex.Mainnet)
	if err != nil {
		t.Fatal(err)
	}
	err = checkProvidersCompliance(ctx, providers, &compat, log)
	if err != nil {
		t.Fatal(err)
	}
}

func (m *MRPCTest) testSimnetEndpoint(endpoints []string, syncBlocks uint64, tFunc func(context.Context, *multiRPCClient)) error {
	dir, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(dir)

	cl, err := m.rpcClient(dir, encode.RandomBytes(32), endpoints, dex.Simnet, false)
	if err != nil {
		return err
	}
	fmt.Println("######## Address:", cl.creds.addr)

	if err := cl.connect(m.ctx); err != nil {
		return fmt.Errorf("connect error: %v", err)
	}

	startHdr, err := cl.bestHeader(m.ctx)
	if err != nil {
		return fmt.Errorf("error getting initial header: %v", err)
	}

	// mine headers
	start := time.Now()
	for {
		m.mine(m.ctx)
		hdr, err := cl.bestHeader(m.ctx)
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
		case <-m.ctx.Done():
			return context.Canceled
		}
	}

	if tFunc != nil {
		tFunc(m.ctx, cl)
	}

	time.Sleep(time.Second)

	return nil
}

func (m *MRPCTest) harnessCmd(ctx context.Context, exe string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, exe, args...)
	cmd.Dir = m.harnessDirectory
	op, err := cmd.CombinedOutput()
	return string(op), err
}

func (m *MRPCTest) mine(ctx context.Context) error {
	_, err := m.harnessCmd(ctx, "./mine-alpha", "1")
	return err
}

func (m *MRPCTest) readProviderFile(t *testing.T, net dex.Network) (seed []byte, providers []string) {
	t.Helper()
	var err error
	seed, providers, err = getFileCredentials(m.chain, m.credentialsFile, net)
	if err != nil {
		t.Fatalf("Error retreiving credentials from file at %q: %v", m.credentialsFile, err)
	}
	return
}
