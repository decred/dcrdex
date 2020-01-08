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
)

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
	// package. The ui.Config struct is used both here and in ui. Could create a
	// types package used by both, but doing it this way works for now.
	cfg, err := ui.Configure()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configration error: ", err)
		return
	}

	// If --notui is specified, don't create the tview application. Initialize
	// logging with the standard stdout logger.
	if cfg.NoTUI {
		logStdout := func(msg []byte) {
			os.Stdout.Write(msg)
		}
		// At least one of --rpc or --web must be specified.
		if !cfg.RPCOn && !cfg.WebOn {
			fmt.Fprintf(os.Stderr, "Cannot run without TUI unless --rpc and/or --web is specified")
			return
		}
		ui.InitLogging(logStdout)
		clientCore := core.New(appCtx, ui.NewLogger("CORE", nil))
		var wg sync.WaitGroup
		if cfg.RPCOn {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rpcserver.Run(appCtx, clientCore, cfg.RPCAddr, ui.NewLogger("RPC", nil))
			}()
		}
		if cfg.WebOn {
			wg.Add(1)
			go func() {
				defer wg.Done()
				webserver.Run(appCtx, clientCore, cfg.WebAddr, ui.NewLogger("WEB", nil))
			}()
		}
		wg.Wait()
		// Close closes the log rotator.
		ui.Close()
		return
	}
	// Run in TUI mode.
	ui.Run(appCtx)
}
