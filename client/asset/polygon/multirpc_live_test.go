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
		"https://polygon-rpc.com",
		"https://rpc-mainnet.matic.quiknode.pro",
		"https://gateway.tenderly.co/public/polygon",
		"https://polygon-bor-rpc.publicnode.com",
		"https://polygon.drpc.org",
		"https://1rpc.io/matic",
		"https://polygon-public.nodies.app",
		"https://endpoints.omniatech.io/v1/matic/mainnet/public",
		"https://polygon.blockpi.network/v1/rpc/public",
		"https://polygon.meowrpc.com",
		"https://poly-rpc.gateway.pokt.network",
		"https://matic-mainnet.chainstacklabs.com",
		"https://polygon.rpc.blxrbdn.com",
		"https://g.w.lavanet.xyz:443/gateway/polygon1/rpc-http/f7ee0000000000000000000000000000",
		"https://rpc-mainnet.matic.network",
		"https://public.stackup.sh/api/v1/node/polygon-mainnet",
	}
	mt.TestFreeServers(t, freeServers, dex.Mainnet)
}

func TestFreeTestnetServers(t *testing.T) {
	// https://chainlist.org/chain/80002
	freeServers := []string{
		"https://polygon-amoy-bor-rpc.publicnode.com",
		"https://rpc-amoy.polygon.technology",
		"https://polygon-amoy.blockpi.network/v1/rpc/public",
		"https://polygon-amoy.drpc.org",
		"wss://polygon-amoy-bor-rpc.publicnode.com",
	}
	mt.TestFreeServers(t, freeServers, dex.Testnet)
}

func TestMainnetCompliance(t *testing.T) {
	mt.TestMainnetCompliance(t)
}

func TestTestnetFees(t *testing.T) {
	mt.FeeHistory(t, dex.Testnet, 3, 90)
}

func TestBlockStats(t *testing.T) {
	mt.BlockStats(t, 5, 1024, dex.Mainnet)
}

func TestTestnetBlockStats(t *testing.T) {
	mt.BlockStats(t, 5, 1024, dex.Testnet)
}

func TestFees(t *testing.T) {
	mt.FeeHistory(t, dex.Mainnet, 3, 365)
}

func TestReceiptsHaveEffectiveGasPrice(t *testing.T) {
	mt.TestReceiptsHaveEffectiveGasPrice(t)
}
