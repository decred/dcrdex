//go:build rpclive

package polygon

import (
	"context"
	"os"
	"testing"

	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/dex"
)

const (
	alphaHTTPPort = "48296"
	alphaWSPort   = "34983"
)

var mt *eth.MRPCTest

func TestMain(m *testing.M) {
	ctx, shutdown := context.WithCancel(context.Background())
	mt = eth.NewMRPCTest(ctx, ChainConfig, NetworkCompatibilityData, "polygon")
	doIt := func() int {
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func TestHTTP(t *testing.T) {
	mt.TestHTTP(t, alphaHTTPPort)
}

func TestWS(t *testing.T) {
	mt.TestWS(t, alphaWSPort)
}

func TestWSTxLogs(t *testing.T) {
	mt.TestWSTxLogs(t, alphaWSPort)
}

func TestSimnetMultiRPCClient(t *testing.T) {
	mt.TestSimnetMultiRPCClient(t, alphaWSPort, alphaHTTPPort)
}

func TestMonitorTestnet(t *testing.T) {
	mt.TestMonitorNet(t, dex.Testnet)
}

func TestMonitorMainnet(t *testing.T) {
	mt.TestMonitorNet(t, dex.Mainnet)
}

func TestRPCMainnet(t *testing.T) {
	mt.TestRPC(t, dex.Mainnet)
}

func TestRPCTestnet(t *testing.T) {
	mt.TestRPC(t, dex.Testnet)
}

func TestFreeServers(t *testing.T) {
	// https://www.alchemy.com/chain-connect/chain/polygon-pos
	// https://chainlist.org/?search=Polygon+Mainnet
	freeServers := []string{
		// Passing
		"https://1rpc.io/matic",
		"https://rpc.ankr.com/polygon",
		"https://polygon-mainnet.public.blastapi.io",
		"https://polygon.blockpi.network/v1/rpc/public",
		"https://polygon.llamarpc.com",
		"https://rpc-mainnet.maticvigil.com",
		"https://endpoints.omniatech.io/v1/matic/mainnet/public",
		"https://rpc-mainnet.matic.quiknode.pro",
		"https://gateway.tenderly.co/public/polygon",
		// Failing
		"https://matic-mainnet-full-rpc.bwarelabs.com", // connect error: failed to connect to even a single provider among: bwarelabs.com
		"https://polygon.api.onfinality.io/public",     // "BalanceAt" error: Too Many Requests, Please apply an OnFinality API key or contact us to receive a higher rate limit
		"https://poly-rpc.gateway.pokt.network",        // connect error: failed to connect to even a single provider among: pokt.network
		"https://polygon-rpc.com",                      // "TransactionReceipt" error: not found
		"https://polygon.meowrpc.com",                  // "TransactionReceipt" error: not found
		"wss://polygon.drpc.org",                       // "TransactionReceipt" error: Unable to perform request
		"https://polygon.rpc.blxrbdn.com",              // "TransactionReceipt" error: not found
		"https://g.w.lavanet.xyz:443/gateway/polygon1/rpc-http/f7ee0000000000000000000000000000", // "TransactionReceipt" error: not found
		"https://rpc-mainnet.matic.network",                     // connect error: failed to connect to even a single provider among: matic.network
		"wss://polygon-bor-rpc.publicnode.com",                  // "TransactionReceipt" error: not found
		"https://public.stackup.sh/api/v1/node/polygon-mainnet", // "TransactionReceipt" error: not found
		"https://matic-mainnet.chainstacklabs.com",              // connect error: failed to connect to even a single provider among: chainstacklabs.com

	}
	mt.TestFreeServers(t, freeServers, dex.Mainnet)
}

func TestFreeTestnetServers(t *testing.T) {
	// https://wiki.polygon.technology/docs/pos/reference/rpc-endpoints/
	// https://www.alchemy.com/chain-connect/chain/mumbai
	// https://chainlist.org/chain/80001
	freeServers := []string{
		// Passing
		"https://rpc.ankr.com/polygon_mumbai",
		"https://polygon-testnet.public.blastapi.io",
		"https://polygon-mumbai.blockpi.network/v1/rpc/public",
		"https://endpoints.omniatech.io/v1/matic/mumbai/public",
		// Failing
		"https://matic-testnet-archive-rpc.bwarelabs.com",                                         // connect error: failed to connect to even a single provider among: bwarelabs.com
		"https://matic-mumbai.chainstacklabs.com",                                                 // connect error: failed to connect to even a single provider among: chainstacklabs.com
		"https://g.w.lavanet.xyz:443/gateway/polygon1t/rpc-http/f7ee0000000000000000000000000000", // "TransactionReceipt" error: not found
		"https://rpc-mumbai.maticvigil.com",                                                       // connect error: failed to connect to even a single provider among: maticvigil.com
		"wss://polygon-mumbai-bor-rpc.publicnode.com",                                             // "TransactionReceipt" error: not found
		"https://polygon-mumbai-pokt.nodies.app",                                                  // "TransactionReceipt" error: not found
	}
	mt.TestFreeServers(t, freeServers, dex.Testnet)
}

func TestMainnetCompliance(t *testing.T) {
	mt.TestMainnetCompliance(t)
}
