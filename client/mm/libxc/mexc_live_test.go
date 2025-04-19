//go:build mexclive

// To run these live tests against the MEXC API:
// 1. Set the required environment variables:
//    export MEXC_API_KEY="your_api_key"
//    export MEXC_SECRET_KEY="your_secret_key"
// 2. Run the tests with the 'mexclive' build tag:
//    go test -v -tags mexclive ./...
// WARNING: Running live tests interacts with the real exchange and may involve real funds.
// Use dedicated, restricted API keys and proceed with caution, especially on mainnet.

package libxc

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	_ "decred.org/dcrdex/client/asset/importall" // Import asset drivers
	"decred.org/dcrdex/dex"
)

var (
	mexcLogger    = dex.StdOutLogger("MEXCLIVE", dex.LevelTrace)
	mexcNet       = dex.Mainnet // Default to mainnet
	mexcAPIKey    string
	mexcSecretKey string
	// Add other flags for specific test parameters as needed
	// e.g., testAssetID uint64
)

// TestMain handles setup, parses flags, and runs tests.
func TestMain(m *testing.M) {
	var testnet bool
	flag.BoolVar(&testnet, "testnet", false, "use testnet")
	// Consider adding explicit key/secret flags or rely solely on ENV VARS
	// flag.StringVar(&mexcAPIKey, "apikey", "", "MEXC API Key")
	// flag.StringVar(&mexcSecretKey, "apisecret", "", "MEXC API Secret")
	flag.Parse()

	if testnet {
		mexcNet = dex.Testnet
		// TODO: Update mexc.go constructor and constants to actually use testnet URLs if defined
		mexcLogger.Warnf("MEXC Testnet not fully supported yet in implementation, URLs might point to mainnet.")
	}

	// Prefer environment variables for secrets
	if k := os.Getenv("MEXC_API_KEY"); k != "" {
		mexcAPIKey = k
	}
	if s := os.Getenv("MEXC_SECRET_KEY"); s != "" {
		mexcSecretKey = s
	}

	if mexcAPIKey == "" || mexcSecretKey == "" {
		mexcLogger.Errorf("MEXC_API_KEY and MEXC_SECRET_KEY environment variables must be set for live tests")
		// os.Exit(1) // Exit if keys aren't set? Or just skip tests? Skipping is safer.
		mexcLogger.Warnf("Skipping live tests as API credentials are not set.")
		return // Don't run tests
	}

	// Run tests
	os.Exit(m.Run())
}

// tNewMEXC creates a new MEXC client instance configured for live testing.
func tNewMEXC(t *testing.T) *mexc {
	t.Helper()
	cfg := &CEXConfig{
		Net:       mexcNet,
		APIKey:    mexcAPIKey,
		SecretKey: mexcSecretKey,
		Logger:    mexcLogger,
		Notify: func(n interface{}) {
			mexcLogger.Infof("Notification sent: %+v", n)
		},
	}
	mexClient, err := newMEXC(cfg)
	require.NoError(t, err, "newMEXC should not return an error")
	require.NotNil(t, mexClient, "newMEXC result should not be nil")
	return mexClient
}

// TestMEXCLiveConnect performs a basic connection test.
func TestMEXCLiveConnect(t *testing.T) {
	if mexcAPIKey == "" || mexcSecretKey == "" {
		t.Skip("Skipping live tests: MEXC API credentials not set")
	}

	mexClient := tNewMEXC(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second) // Timeout for connection
	defer cancel()

	var connectWg *sync.WaitGroup
	var err error
	done := make(chan struct{})
	go func() {
		connectWg, err = mexClient.Connect(ctx)
		close(done)
	}()

	select {
	case <-done:
		require.NoError(t, err, "mexClient.Connect should not return an error")
		require.NotNil(t, connectWg, "Connect WaitGroup should not be nil")
		mexcLogger.Infof("MEXC Connect successful.")
		// Optionally perform a simple post-connect check like fetching balances
		_, balanceErr := mexClient.Balances(ctx)
		require.NoError(t, balanceErr, "mexClient.Balances should not return an error post-connect")
		mexcLogger.Infof("MEXC Balances fetch successful.")
	case <-ctx.Done():
		t.Fatalf("mexClient.Connect timed out: %v", ctx.Err())
	}

	// Cleanly disconnect? The interface doesn't explicitly require a Disconnect method,
	// relies on context cancellation. Cancelling the context should trigger shutdown.
	cancel()         // Signal shutdown via context
	connectWg.Wait() // Wait for Connect goroutines to finish
	mexcLogger.Infof("MEXC Connect WaitGroup finished.")
}

// TestMEXCLiveBalances fetches and logs account balances.
func TestMEXCLiveBalances(t *testing.T) {
	if mexcAPIKey == "" || mexcSecretKey == "" {
		t.Skip("Skipping live tests: MEXC API credentials not set")
	}

	mexClient := tNewMEXC(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // Longer timeout needed for Connect + Balances
	defer cancel()

	// Connect first
	var connectWg *sync.WaitGroup
	var connectErr error
	doneConnect := make(chan struct{})
	go func() {
		connectWg, connectErr = mexClient.Connect(ctx)
		close(doneConnect)
	}()

	select {
	case <-doneConnect:
		require.NoError(t, connectErr, "mexClient.Connect failed")
		require.NotNil(t, connectWg, "Connect WaitGroup is nil")
	case <-ctx.Done():
		t.Fatalf("mexClient.Connect timed out: %v", ctx.Err())
	}

	// Fetch Balances
	balances, err := mexClient.Balances(ctx)
	require.NoError(t, err, "mexClient.Balances failed")
	require.NotNil(t, balances, "Balances map should not be nil")

	mexcLogger.Infof("Retrieved %d balances:", len(balances))
	for assetID, bal := range balances {
		symbol := dex.BipIDSymbol(assetID) // Assuming dex package is imported
		mexcLogger.Infof("  %s (ID: %d): Available=%d, Locked=%d", symbol, assetID, bal.Available, bal.Locked)
	}

	// Shutdown
	cancel()         // Signal shutdown via context
	connectWg.Wait() // Wait for Connect goroutines to finish
	mexcLogger.Infof("MEXC Connect WaitGroup finished for Balances test.")
}

// TestMEXCLiveMarkets fetches and logs supported markets.
func TestMEXCLiveMarkets(t *testing.T) {
	if mexcAPIKey == "" || mexcSecretKey == "" {
		t.Skip("Skipping live tests: MEXC API credentials not set")
	}

	mexClient := tNewMEXC(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // Longer timeout for Connect + Markets
	defer cancel()

	// Connect first
	var connectWg *sync.WaitGroup
	var connectErr error
	doneConnect := make(chan struct{})
	go func() {
		connectWg, connectErr = mexClient.Connect(ctx)
		close(doneConnect)
	}()

	select {
	case <-doneConnect:
		require.NoError(t, connectErr, "mexClient.Connect failed")
		require.NotNil(t, connectWg, "Connect WaitGroup is nil")
	case <-ctx.Done():
		t.Fatalf("mexClient.Connect timed out: %v", ctx.Err())
	}

	// Fetch Matched Markets
	matchedMarkets, err := mexClient.MatchedMarkets(ctx)
	require.NoError(t, err, "mexClient.MatchedMarkets failed")
	require.NotNil(t, matchedMarkets, "MatchedMarkets slice should not be nil")
	mexcLogger.Infof("Retrieved %d matched markets:", len(matchedMarkets))
	for _, mm := range matchedMarkets {
		mexcLogger.Infof("  Matched Market: ID=%s, BaseID=%d, QuoteID=%d, Slug=%s", mm.MarketID, mm.BaseID, mm.QuoteID, mm.Slug)
	}

	// Fetch Markets (which includes MarketDay data)
	markets, err := mexClient.Markets(ctx)
	require.NoError(t, err, "mexClient.Markets failed")
	require.NotNil(t, markets, "Markets map should not be nil")
	mexcLogger.Infof("Retrieved %d markets with data:", len(markets))
	for marketID, m := range markets {
		dayInfo := "<nil>"
		if m.Day != nil {
			dayInfo = fmt.Sprintf("Vol:%.2f, Last:%.8f, Change:%.2f%%", m.Day.QuoteVol, m.Day.LastPrice, m.Day.PriceChangePct)
		}
		mexcLogger.Infof("  Market %s (Base:%d, Quote:%d): Day=[%s]", marketID, m.BaseID, m.QuoteID, dayInfo)
	}

	// Shutdown
	cancel()         // Signal shutdown via context
	connectWg.Wait() // Wait for Connect goroutines to finish
	mexcLogger.Infof("MEXC Connect WaitGroup finished for Markets test.")
}

// TestMEXCLiveGetDepositAddress tests fetching a deposit address.
func TestMEXCLiveGetDepositAddress(t *testing.T) {
	if mexcAPIKey == "" || mexcSecretKey == "" {
		t.Skip("Skipping live tests: MEXC API credentials not set")
	}

	// Define the asset to test (e.g., DCR = 42, BTC = 0, USDT.polygon = 966001)
	// Use a flag or hardcode an asset ID known to be supported by your MEXC account and the implementation.
	// Using DCR (42) as an example.
	const testAssetID = 42

	mexClient := tNewMEXC(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // Timeout for Connect + GetDepositAddress
	defer cancel()

	// Connect first
	var connectWg *sync.WaitGroup
	var connectErr error
	doneConnect := make(chan struct{})
	go func() {
		connectWg, connectErr = mexClient.Connect(ctx)
		close(doneConnect)
	}()

	select {
	case <-doneConnect:
		require.NoError(t, connectErr, "mexClient.Connect failed")
		require.NotNil(t, connectWg, "Connect WaitGroup is nil")
	case <-ctx.Done():
		t.Fatalf("mexClient.Connect timed out: %v", ctx.Err())
	}

	// Fetch Deposit Address
	assetSymbol := dex.BipIDSymbol(testAssetID)
	mexcLogger.Infof("Attempting to get deposit address for %s (ID: %d)...", assetSymbol, testAssetID)
	depositAddress, err := mexClient.GetDepositAddress(ctx, testAssetID)
	require.NoError(t, err, "mexClient.GetDepositAddress failed for %s", assetSymbol)
	require.NotEmpty(t, depositAddress, "Deposit address should not be empty for %s", assetSymbol)

	mexcLogger.Infof("Successfully retrieved deposit address for %s: %s", assetSymbol, depositAddress)

	// Shutdown
	cancel()         // Signal shutdown via context
	connectWg.Wait() // Wait for Connect goroutines to finish
	mexcLogger.Infof("MEXC Connect WaitGroup finished for GetDepositAddress test.")
}
