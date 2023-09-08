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
	// https://www.alchemy.com/chain-connect/chain/ethereum
	// Passing 09-10-2023
	"https://rpc.builder0x69.io",
	"https://rpc.ankr.com/eth",
	"https://eth-mainnet.public.blastapi.io",
	"https://ethereum.blockpi.network/v1/rpc/public",
	"https://rpc.flashbots.net",
	"https://eth.api.onfinality.io/public",
	"https://eth-mainnet-public.unifra.io",
	// Not passing 09-10-2023
	"https://api.securerpc.com/v1",
	"https://virginia.rpc.blxrbdn.com",
	"https://eth.rpc.blxrbdn.com",
	"https://1rpc.io/eth",
	"https://g.w.lavanet.xyz:443/gateway/eth/rpc-http/f7ee0000000000000000000000000000",
	"https://api.mycryptoapi.com/eth",
	"https://rpc.mevblocker.io",
	"https://eth-mainnet.nodereal.io/v1/1659dfb40aa24bbb8153a677b98064d7",
	"https://endpoints.omniatech.io/v1/eth/mainnet/public",
	"https://uk.rpc.blxrbdn.com",
	"https://singapore.rpc.blxrbdn.com",
	"https://eth.llamarpc.com",
	"https://ethereum.publicnode.com",
	"https://api.zmok.io/mainnet/oaen6dy8ff6hju9k",
	"https://ethereumnodelight.app.runonflux.io",
	"https://eth-rpc.gateway.pokt.network",
	"https://main-light.eth.linkpool.io",
	"https://rpc.payload.de",
	"https://cloudflare-eth.com",
	"https://main-rpc.linkpool.io/",
	"https://nodes.mewapi.io/rpc/eth",
}

func TestFreeServers(t *testing.T) {
	mt.TestFreeServers(t, freeServers, dex.Mainnet)
}

func TestMainnetCompliance(t *testing.T) {
	mt.TestMainnetCompliance(t)
}
