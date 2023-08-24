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
	freeServers := []string{
		"wss://polygon-mainnet.public.blastapi.io",
		"https://polygon.blockpi.network/v1/rpc/public",
		"https://polygon.publicnode.com",
		"https://rpc.ankr.com/polygon",
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
		"https://rpc-mumbai.maticvigil.com",

		// Not passing
		"https://polygon-mumbai-bor.publicnode.com",
		"https://endpoints.omniatech.io/v1/matic/mumbai/public",
		"https://polygontestapi.terminet.io/rpc",
		"https://matic-mumbai.chainstacklabs.com",
		"https://matic-testnet-archive-rpc.bwarelabs.com",
		"https://g.w.lavanet.xyz:443/gateway/polygon1t/rpc-http/f7ee0000000000000000000000000000",
		"https://api.zan.top/node/v1/polygon/mumbai/public",
	}
	mt.TestFreeServers(t, freeServers, dex.Testnet)
}

func TestMainnetCompliance(t *testing.T) {
	mt.TestMainnetCompliance(t)
}
