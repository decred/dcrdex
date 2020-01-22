// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"

	_ "decred.org/dcrdex/server/asset/btc" // register btc asset
	_ "decred.org/dcrdex/server/asset/dcr" // register dcr asset
	_ "decred.org/dcrdex/server/asset/ltc" // register ltc asset
	dexsrv "decred.org/dcrdex/server/dex"
)

func mainCore(ctx context.Context) error {
	// Parse the configuration file, and setup logger.
	cfg, opts, err := loadConfig()
	if err != nil {
		fmt.Printf("Failed to load dcrdata config: %s\n", err.Error())
		return err
	}
	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	if opts.CPUProfile != "" {
		var f *os.File
		f, err = os.Create(opts.CPUProfile)
		if err != nil {
			return err
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	// HTTP profiler
	if opts.HTTPProfile {
		log.Warnf("Starting the HTTP profiler on path /debug/pprof/.")
		// http pprof uses http.DefaultServeMux
		http.Handle("/", http.RedirectHandler("/debug/pprof/", http.StatusSeeOther))
		go func() {
			if err := http.ListenAndServe(":9232", nil); err != nil {
				log.Errorf("ListenAndServe failed for http/pprof: %v", err)
			}
		}()
	}

	// Display app version.
	log.Infof("%s version %v (Go version %s)", AppName, Version(), runtime.Version())
	log.Infof("dcrdex starting for network: %s", cfg.Network)

	// Load the market and asset configurations for the given network.
	markets, assets, err := loadMarketConfFile(cfg.Network, cfg.MarketsConfPath)
	if err != nil {
		return fmt.Errorf("failed to load market and asset config %q: %v",
			cfg.MarketsConfPath, err)
	}
	log.Infof("Found %d assets, loaded %d markets, for network %s",
		len(assets), len(markets), strings.ToUpper(cfg.Network.String()))

	// Create the DEX manager.
	dexConf := &dexsrv.DexConf{
		LogBackend: cfg.LogMaker,
		Markets:    markets,
		Assets:     assets,
		Network:    cfg.Network,
		DBConf: &dexsrv.DBConf{
			DBName: cfg.DBName,
			Host:   cfg.DBHost,
			User:   cfg.DBUser,
			Port:   cfg.DBPort,
			Pass:   cfg.DBPass,
		},
		RegFeeXPub:       cfg.RegFeeXPub,
		RegFeeAmount:     cfg.RegFeeAmount,
		RegFeeConfirms:   cfg.RegFeeConfirms,
		BroadcastTimeout: cfg.BroadcastTimeout,
		DEXPrivKey:       cfg.DEXPrivKey,
		CommsCfg: &dexsrv.RPCConfig{
			RPCCert:     cfg.RPCCert,
			RPCKey:      cfg.RPCKey,
			ListenAddrs: cfg.RPCListen,
			AltDNSNames: cfg.AltDNSNames,
		},
	}
	dexMan, err := dexsrv.NewDEX(dexConf)
	if err != nil {
		return err
	}

	log.Info("The DEX is running. Hit CTRL+C to quit...")
	<-ctx.Done()

	log.Info("Stopping DEX...")
	dexMan.Stop()
	log.Info("Bye!")

	return nil
}

func main() {
	// Create a context that is cancelled when a shutdown request is received
	// via requestShutdown.
	ctx := withShutdownCancel(context.Background())
	// Listen for both interrupt signals (e.g. CTRL+C) and shutdown requests
	// (requestShutdown calls).
	go shutdownListener()

	err := mainCore(ctx)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	os.Exit(0)
}
