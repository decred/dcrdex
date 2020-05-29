// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"

	_ "decred.org/dcrdex/client/asset/btc" // register btc asset
	_ "decred.org/dcrdex/client/asset/dcr" // register dcr asset
	"decred.org/dcrdex/client/cmd/dexc/ui"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/client/webserver"
	"github.com/decred/slog"
)

var log slog.Logger

func main() {
	appCtx, cancel := context.WithCancel(context.Background())
	// Catch ctrl+c. This will need to be smarter eventually, probably displaying
	// a modal dialog to confirm closing, especially if servers are running or if
	// swaps are in negotiation.
	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		cancel()
	}()

	// Parse configuration and set up initial logging.
	//
	// DRAFT NOTE: It's a little odd that the Configure function is from the ui
	// package. The ui.Config struct is used both here and in ui. Could create  a
	// types package used by both, but doing it this way works for now.
	cfg, err := ui.Configure()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configration error: %v\n", err)
		os.Exit(1)
	}

	if cfg.TUI {
		// Run in TUI mode.
		ui.Run(appCtx)
		os.Exit(0)
	}

	// If explicitly running without web server then you must run the rpc
	// server or the terminal ui.
	if cfg.NoWeb && !cfg.RPCOn {
		fmt.Fprintf(os.Stderr, "Cannot run without web server unless --rpc or --tui is specified\n")
		os.Exit(1)
	}

	// If --tui is not specified, don't create the tview application. Initialize
	// logging with the standard stdout logger.
	logStdout := func(msg []byte) {
		os.Stdout.Write(msg)
	}
	logMaker := ui.InitLogging(logStdout, cfg.DebugLevel)
	core.UseLoggerMaker(logMaker)
	log = logMaker.Logger("DEXC")

	clientCore, err := core.New(&core.Config{
		DBPath: cfg.DBPath, // global set in config.go
		Net:    cfg.Net,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating client core: %v\n", err)
		os.Exit(1)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		clientCore.Run(appCtx)
		wg.Done()
	}()

	if cfg.RPCOn {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rpcserver.SetLogger(logMaker.Logger("RPC"))
			rpcCfg := &rpcserver.Config{clientCore, cfg.RPCAddr, cfg.RPCUser, cfg.RPCPass, cfg.RPCCert, cfg.RPCKey}
			rpcSrv, err := rpcserver.New(rpcCfg)
			if err != nil {
				log.Errorf("Error constructing rpc server: %v", err)
				os.Exit(1)
			}

			//rpcSrv.Run(appCtx)

			rpcSrv.Start(appCtx)

		}()
	}

	if !cfg.NoWeb {
		wg.Add(1)
		go func() {
			defer wg.Done()
			webSrv, err := webserver.New(clientCore, cfg.WebAddr, logMaker.Logger("WEB"), cfg.ReloadHTML)
			if err != nil {
				log.Errorf("Error starting web server: %v", err)
				os.Exit(1)
			}
			webSrv.Run(appCtx)
		}()
	}

	wg.Wait()
	ui.Close()
}
