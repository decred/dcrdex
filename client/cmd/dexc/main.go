// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	_ "decred.org/dcrdex/client/asset/btc" // register btc asset
	_ "decred.org/dcrdex/client/asset/dcr" // register dcr asset
	_ "decred.org/dcrdex/client/asset/ltc" // register ltc asset
	"decred.org/dcrdex/client/cmd/dexc/ui"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/client/webserver"
	"decred.org/dcrdex/dex"
	"github.com/decred/slog"
)

var log slog.Logger

func main() {
	appCtx, cancel := context.WithCancel(context.Background())

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
	logMaker := ui.InitLogging(logStdout, cfg.DebugLevel, !cfg.LocalLogs)
	core.UseLoggerMaker(logMaker)
	log = logMaker.Logger("DEXC")
	if !cfg.LocalLogs {
		log.Infof("Logging with UTC time stamps. Current local time is %v", time.Now().Local().Format("15:04:05 MST"))
	}

	clientCore, err := core.New(&core.Config{
		DBPath: cfg.DBPath, // global set in config.go
		Net:    cfg.Net,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating client core: %v\n", err)
		os.Exit(1)
	}

	// Catch interrupt signal (e.g. ctrl+c), prompting to shutdown if the user
	// is logged in, and there are active orders or matches.
	killChan := make(chan os.Signal)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		for range killChan {
			if clientCore.PromptShutdown() {
				cancel()
				return
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		clientCore.Run(appCtx)
		cancel() // in the event that Run returns prematurely prior to context cancellation
		wg.Done()
	}()

	if cfg.RPCOn {
		rpcserver.SetLogger(logMaker.Logger("RPC"))
		rpcCfg := &rpcserver.Config{
			Core: clientCore,
			Addr: cfg.RPCAddr,
			User: cfg.RPCUser,
			Pass: cfg.RPCPass,
			Cert: cfg.RPCCert,
			Key:  cfg.RPCKey,
		}
		rpcSrv, err := rpcserver.New(rpcCfg)
		if err != nil {
			log.Errorf("Error creating rpc server: %v", err)
			cancel()
			goto done
		}
		cm := dex.NewConnectionMaster(rpcSrv)
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = cm.Connect(appCtx)
			if err != nil {
				log.Errorf("Error starting rpc server: %v", err)
				cancel()
				return
			}
			cm.Wait()
		}()
	}

	if !cfg.NoWeb {
		webSrv, err := webserver.New(clientCore, cfg.WebAddr, logMaker.Logger("WEB"), cfg.ReloadHTML)
		if err != nil {
			log.Errorf("Error creating web server: %v", err)
			cancel()
			goto done
		}
		cm := dex.NewConnectionMaster(webSrv)
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = cm.Connect(appCtx)
			if err != nil {
				log.Errorf("Error starting web server: %v", err)
				cancel()
				return
			}
			cm.Wait()
		}()
	}

done:
	wg.Wait()
	ui.Close()
	log.Info("Exiting dexc main.")
}
