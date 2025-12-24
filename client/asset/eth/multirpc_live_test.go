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
	"https://ethereum-rpc.publicnode.com",
	"https://eth-mainnet.nodereal.io/v1/1659dfb40aa24bbb8153a677b98064d7",
	"https://ethereum.publicnode.com",
	"https://eth.api.onfinality.io/public",
	"https://eth.drpc.org",
	"https://1rpc.io/eth",
	"https://ethereum.blockpi.network/v1/rpc/public",
	"https://rpc.flashbots.net",
	"https://rpc.builder0x69.io",
	"https://eth-mainnet.gateway.pokt.network/v1/5f3453978e354ab992c4da79",
	"https://nodes.mewapi.io/rpc/eth",
	"https://eth-mainnet-public.unifra.io",
	"https://cloudflare-eth.com/",
}

func TestFreeServers(t *testing.T) {
	mt.TestFreeServers(t, freeServers, dex.Mainnet)
}

var freeTestnetServers = []string{
	"https://ethereum-sepolia-rpc.publicnode.com",
	"https://sepolia.drpc.org",
	"https://rpc.sepolia.org",
	"https://1rpc.io/sepolia",
	"https://ethereum-sepolia.blockpi.network/v1/rpc/public",
	"https://endpoints.omniatech.io/v1/eth/sepolia/public",
	"https://rpc2.sepolia.org",
	"https://rpc-sepolia.rockx.com",
	"https://rpc.sepolia.ethpandaops.io",
	"https://eth-sepolia-public.unifra.io",
	"https://relay-sepolia.flashbots.net",
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
