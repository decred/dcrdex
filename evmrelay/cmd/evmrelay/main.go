// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

// evmrelay is a relay server for gasless EIP-712 signed redemptions.
// It receives signed calldata from DEX clients and submits the transactions
// on-chain, earning a relay fee deducted from the redeemed ETH.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"decred.org/dcrdex/dex"
	basenet "decred.org/dcrdex/dex/networks/base"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
	"github.com/decred/slog"
)

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "relay.json"
	}
	return filepath.Join(home, ".evmrelay", "relay.json")
}

func mainErr() error {
	var configPath string
	var logLevel string
	simnet := flag.Bool("simnet", false, "simnet mode")
	testnet := flag.Bool("testnet", false, "testnet mode")
	flag.StringVar(&configPath, "config", defaultConfigPath(), "Path to relay config file")
	flag.StringVar(&logLevel, "log", "", "Log level (overrides config)")
	flag.Parse()

	configData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("error reading config file %q: %w", configPath, err)
	}

	var cfg relayConfig
	if err := json.Unmarshal(configData, &cfg); err != nil {
		return fmt.Errorf("error parsing config: %w", err)
	}

	switch {
	case *simnet:
		cfg.Net = dex.Simnet
	case *testnet:
		cfg.Net = dex.Testnet
	default:
		cfg.Net = dex.Mainnet
	}

	if cfg.Addr == "" {
		cfg.Addr = ":21232"
	}

	// Env var overrides config for private key.
	if envKey := os.Getenv("EVMRELAY_PRIVKEY"); envKey != "" {
		cfg.PrivKey = envKey
	}
	if cfg.PrivKey == "" {
		return fmt.Errorf("private key must be set via config or EVMRELAY_PRIVKEY env var")
	}

	// Override log level if specified on command line.
	if logLevel != "" {
		cfg.LogLevel = logLevel
	}
	if cfg.LogLevel != "" {
		lvl, found := slog.LevelFromString(cfg.LogLevel)
		if found {
			log = dex.StdOutLogger("RELAY", lvl)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-killChan
		log.Infof("Received signal (%s). Shutting down...", sig)
		cancel()
	}()

	// Populate simnet contract addresses from harness files.
	dexeth.MaybeReadSimnetAddrs()
	dexpolygon.MaybeReadSimnetAddrs()
	basenet.MaybeReadSimnetAddrs()

	srv, err := newRelayServer(ctx, &cfg)
	if err != nil {
		return fmt.Errorf("error creating relay server: %w", err)
	}

	return srv.run(ctx)
}
