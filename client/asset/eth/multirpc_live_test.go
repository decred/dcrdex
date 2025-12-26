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
	// Verified working (2025-12-26)
	"https://eth-mainnet.nodereal.io/v1/1659dfb40aa24bbb8153a677b98064d7", // NodeReal
	"https://eth.api.onfinality.io/public",                                // OnFinality
	"https://eth-mainnet.public.blastapi.io",                              // Blast API
	"https://ethereum-rpc.publicnode.com",                                 // PublicNode
}

func TestFreeServers(t *testing.T) {
	mt.TestFreeServers(t, freeServers, dex.Mainnet)
}

var freeTestnetServers = []string{
	// Verified working (2025-12-26)
	"https://ethereum-sepolia-rpc.publicnode.com",          // PublicNode
	"https://sepolia.drpc.org",                             // dRPC
	"https://endpoints.omniatech.io/v1/eth/sepolia/public", // Omniatech
	"https://rpc-sepolia.rockx.com",                        // RockX
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
