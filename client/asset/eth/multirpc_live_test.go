//go:build rpclive

package eth

import (
	"context"
	"os"
	"testing"

	"decred.org/dcrdex/dex"
)

const (
	alphaHTTPPort = "38553"
	alphaWSPort   = "38554"
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
	// https://www.alchemy.com/chain-connect/chain/ethereum
	// Passing 03-26-2024
	"https://rpc.builder0x69.io",                                          // Limits unknown
	"https://eth.drpc.org",                                                // 210 million Compute Units (CU) per 30-day period - 20 CU/req
	"https://rpc.ankr.com/eth",                                            // 30 req per second, no WebSockets (premium-only)
	"https://ethereum.blockpi.network/v1/rpc/public",                      // 10 req per sec, no WebSockets (premium-only)
	"https://rpc.flashbots.net",                                           // Limits unknown
	"wss://eth.llamarpc.com",                                              // Limits unknown
	"https://eth-mainnet.nodereal.io/v1/1659dfb40aa24bbb8153a677b98064d7", // Limits might be 100M compute units at 300 CU/s
	// Failing 03-26-2024
	"https://eth-mainnet.gateway.pokt.network/v1/5f3453978e354ab992c4da79", // connect error: failed to connect to even a single provider among: pokt.network
	"https://ethereum.publicnode.com",                                      // "TransactionReceipt" error: not found
	"https://nodes.mewapi.io/rpc/eth",                                      // connect error: failed to connect to even a single provider among: mewapi.io
	"https://eth.api.onfinality.io/public",                                 // connect error: failed to connect to even a single provider among: onfinality.io
	"https://eth-mainnet-public.unifra.io",                                 // connect error: failed to connect to even a single provider among: unifra.io
	"https://cloudflare-eth.com/",                                          // "SuggestGasTipCap" error: Method not found

}

func TestFreeServers(t *testing.T) {
	mt.TestFreeServers(t, freeServers, dex.Mainnet)
}

var freeTestnetServers = []string{
	// Sepolia
	// Passing 03-27-2024
	"https://rpc.ankr.com/eth_sepolia",
	"https://ethereum-sepolia.blockpi.network/v1/rpc/public",
	"https://sepolia.drpc.org",
	"https://endpoints.omniatech.io/v1/eth/sepolia/public",
	"https://rpc-sepolia.rockx.com",
	"https://rpc.sepolia.org",
	"https://eth-sepolia-public.unifra.io",
	// Failing 03-27-2024
	"https://relay-sepolia.flashbots.net", // connect error: failed to connect to even a single provider among: flashbots.net

	//
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
