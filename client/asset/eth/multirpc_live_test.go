//go:build rpclive

package eth

import (
	"context"
	"os"
	"testing"

	"decred.org/dcrdex/dex"
)

const (
	alphaHTTPPort = "38556"
	alphaWSPort   = "38557"
)

var mt *MRPCTest

func TestMain(m *testing.M) {
	ctx, shutdown := context.WithCancel(context.Background())
	mt = NewMRPCTest(ctx, ChainConfig, NetworkCompatibilityData, "eth")
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

func TestRPC(t *testing.T) {
	mt.TestRPC(t, dex.Mainnet)
}

var freeServers = []string{
	// Verified working (26-12-2025)
	"https://eth-mainnet.nodereal.io/v1/1659dfb40aa24bbb8153a677b98064d7", // NodeReal
	"https://eth.api.onfinality.io/public",                                // OnFinality
	"https://eth-mainnet.public.blastapi.io",                              // Blast API
	"https://ethereum-rpc.publicnode.com",                                 // PublicNode

	// Failing 26-12-2025
	// "https://eth-mainnet.gateway.pokt.network/v1/5f3453978e354ab992c4da79", // connect error: failed to connect to even a single provider among: pokt.network
	// "https://ethereum.publicnode.com",      // "TransactionReceipt" error: not found
	// "https://nodes.mewapi.io/rpc/eth",      // connect error: failed to connect to even a single provider among: mewapi.io
	// "https://eth-mainnet-public.unifra.io", // connect error: failed to connect to even a single provider among: unifra.io
	// "https://cloudflare-eth.com/",                                          // "SuggestGasTipCap" error: Method not found
	// "https://eth.llamarpc.com", 			// FAILED : "HeaderByHash" error: context deadline exceeded
}

func TestFreeServers(t *testing.T) {
	mt.TestFreeServers(t, freeServers, dex.Mainnet)
}

var freeTestnetServers = []string{
	// Verified working (26-12-2025)
	"https://ethereum-sepolia-rpc.publicnode.com",          // PublicNode
	"https://sepolia.drpc.org",                             // dRPC
	"https://endpoints.omniatech.io/v1/eth/sepolia/public", // Omniatech
	"https://rpc-sepolia.rockx.com",                        // RockX

	// Failing 03-27-2024
	// "https://relay-sepolia.flashbots.net", // connect error: failed to connect to even a single provider among: flashbots.net

	// Goerli
	// // Passing 03-26-2024
	// "https://goerli.blockpi.network/v1/rpc/public",
	// "https://goerli.infura.io/v3/9aa3d95b3bc440fa88ea12eaa4456161",
	// // Failing 03-26-2024
	// "https://rpc.ankr.com/eth_goerli",
	// "https://rpc.goerli.eth.gateway.fm",
	// "https://goerli.infura.io/v3/9aa3d95b3bc440fa88ea12eaa4456161",
	// "https://rpc.goerli.mudit.blog",
	// "https://endpoints.omniatech.io/v1/eth/goerli/public",
	// "https://eth-goerli.api.onfinality.io/public",
	// "https://relay-goerli.flashbots.net",
}

func TestFreeTestnetServers(t *testing.T) {
	mt.TestFreeServers(t, freeTestnetServers, dex.Testnet)
}

func TestMainnetCompliance(t *testing.T) {
	mt.TestMainnetCompliance(t)
}

func TestReceiptsHaveEffectiveGasPrice(t *testing.T) {
	mt.TestReceiptsHaveEffectiveGasPrice(t)
}

func TestBlockStats(t *testing.T) {
	mt.BlockStats(t, 5, 1024, dex.Mainnet)
}

func TestTestnetBlockStats(t *testing.T) {
	mt.BlockStats(t, 5, 1024, dex.Testnet)
}
