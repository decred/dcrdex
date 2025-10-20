//go:build rpclive

package base

import (
	"context"
	"os"
	"testing"

	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/dex"
)

var mt *eth.MRPCTest

func TestMain(m *testing.M) {
	ctx, shutdown := context.WithCancel(context.Background())
	mt = eth.NewMRPCTest(ctx, ChainConfig, NetworkCompatibilityData, "weth.base")
	doIt := func() int {
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
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
		"https://base-rpc.publicnode.com",
		"https://mainnet.base.org",
		"https://base.drpc.org",
		"https://base.llamarpc.com",
		"https://base.api.onfinality.io/public",
	}
	mt.TestFreeServers(t, freeServers, dex.Mainnet)
}

func TestFreeTestnetServers(t *testing.T) {
	freeServers := []string{
		"https://base-sepolia-rpc.publicnode.com",
		"https://sepolia.base.org",
		"https://base-sepolia.drpc.org",
		"https://base-sepolia.api.onfinality.io/public",
		"https://base-sepolia.gateway.tenderly.co",
	}
	mt.TestFreeServers(t, freeServers, dex.Testnet)
}

func TestMainnetCompliance(t *testing.T) {
	mt.TestMainnetCompliance(t)
}

func TestTestnetFees(t *testing.T) {
	mt.FeeHistory(t, dex.Testnet, 3, 90)
}

func TestFees(t *testing.T) {
	mt.FeeHistory(t, dex.Mainnet, 3, 365)
}

func TestReceiptsHaveEffectiveGasPrice(t *testing.T) {
	mt.TestReceiptsHaveEffectiveGasPrice(t)
}

func TestBaseReceiptsHaveEffectiveGasPrice(t *testing.T) {
	mt.TestBaseReceiptsHaveEffectiveGasPrice(t)
}

func TestBaseBlockStats(t *testing.T) {
	mt.BaseBlockStats(t, 5, 1024, dex.Mainnet)
}

func TestBaseTestnetBlockStats(t *testing.T) {
	mt.BaseBlockStats(t, 5, 1024, dex.Testnet)
}
