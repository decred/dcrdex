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

func TestRPC(t *testing.T) {
	mt.TestRPC(t)
}

// var freeServers = []string{
// 	"https://cloudflare-eth.com/", // cloudflare-eth.com "SuggestGasTipCap" error: Method not found
// 	"https://main-rpc.linkpool.io/",
// 	"https://nodes.mewapi.io/rpc/eth",
// 	"https://rpc.flashbots.net/",
// 	"https://rpc.ankr.com/eth", // Passes, but doesn't support SyncProgress, which don't use and just lie about right now.
// 	"https://api.mycryptoapi.com/eth",
// 	"https://ethereumnodelight.app.runonflux.io",
// }

// func TestFreeServers(t *testing.T) {
// 	mt.TestFreeServers(t, freeServers)
// }

func TestMainnetCompliance(t *testing.T) {
	mt.TestMainnetCompliance(t)
}
