// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"

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

	// If --notui is specified, don't create the tview application. Initialize
	// logging with the standard stdout logger.
	if cfg.NoTUI {
		logStdout := func(msg []byte) {
			os.Stdout.Write(msg)
		}
		clientCore, err := core.New(&core.Config{
			DBPath:      cfg.DBPath, // global set in config.go
			LoggerMaker: ui.NewLoggerMaker(nil),
			Certs:       cfg.Certs,
			Net:         cfg.Net,
		})
		if err != nil {
			fmt.Fprint(os.Stderr, "error creating client core: ", err)
			return
		}
		go clientCore.Run(appCtx)

		ui.InitLogging(logStdout)
		// At least one of --rpc or --web must be specified.
		if !cfg.RPCOn && !cfg.WebOn {
			fmt.Fprintf(os.Stderr, "Cannot run without TUI unless --rpc and/or --web is specified")
			return
		}
		var wg sync.WaitGroup
		if cfg.RPCOn {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rpcserver.Run(appCtx, clientCore, cfg.RPCAddr, ui.NewLoggerMaker(logStdout).Logger("RPC"))
			}()
		}
		if cfg.WebOn {
			wg.Add(1)
			go func() {
				defer wg.Done()
				webSrv, err := webserver.New(clientCore, cfg.WebAddr, ui.NewLoggerMaker(logStdout).Logger("WEB"), cfg.ReloadHTML)
				if err != nil {
					log.Errorf("Error starting web server: %v", err)
					return
				}
				webSrv.Run(appCtx)
			}()
		}
		wg.Wait()
		ui.Close()
		return
	}
	// Run in TUI mode.
	ui.Run(appCtx)
}
