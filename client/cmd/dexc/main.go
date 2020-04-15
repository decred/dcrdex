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
		fmt.Fprint(os.Stderr, "configration error: ", err)
		return
	}

	if cfg.TUI {
		// Run in TUI mode.
		ui.Run(appCtx)
		return
	}

	// If --tui is not specified, don't create the tview application. Initialize
	// logging with the standard stdout logger.
	logStdout := func(msg []byte) {
		os.Stdout.Write(msg)
	}
	logMaker := ui.InitLogging(logStdout, cfg.DebugLevel)
	clientCore, err := core.New(&core.Config{
		DBPath:      cfg.DBPath, // global set in config.go
		LoggerMaker: logMaker,
		Certs:       cfg.Certs,
		Net:         cfg.Net,
	})
	if err != nil {
		fmt.Fprint(os.Stderr, "error creating client core: ", err)
		return
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		clientCore.Run(appCtx)
		wg.Done()
	}()
	// At least one of --rpc or --web must be specified.
	if cfg.NoWeb && !cfg.RPCOn {
		fmt.Fprintf(os.Stderr, "Cannot run without web server unless --rpc or --tui is specified\n")
		return
	}
	if cfg.RPCOn {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rpcserver.SetLogger(logMaker.Logger("RPC"))
			rpcCfg := &rpcserver.Config{clientCore, cfg.RPCAddr, cfg.RPCUser, cfg.RPCPass, cfg.RPCCert, cfg.RPCKey}
			rpcSrv, err := rpcserver.New(rpcCfg)
			if err != nil {
				log.Errorf("Error starting rpc server: %v", err)
				return
			}
			rpcSrv.Run(appCtx)
		}()
	}
	if !cfg.NoWeb {
		wg.Add(1)
		go func() {
			defer wg.Done()
			webSrv, err := webserver.New(clientCore, cfg.WebAddr, logMaker.Logger("WEB"), cfg.ReloadHTML)
			if err != nil {
				log.Errorf("Error starting web server: %v", err)
				return
			}
			webSrv.Run(appCtx)
		}()
	}
	wg.Wait()
	ui.Close()
}
