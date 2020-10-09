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
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/client/webserver"
	"decred.org/dcrdex/dex"
)

var (
	appCtx, closeApp = context.WithCancel(context.Background())
)

func main() {

	// Parse configuration.
	cfg, err := configure()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configration error: %v\n", err)
		os.Exit(1)
	}

	// If explicitly running without web server then you must run the rpc
	// server.
	if cfg.NoWeb && !cfg.RPCOn {
		fmt.Fprintf(os.Stderr, "Cannot run without web server unless --rpc is specified\n")
		os.Exit(1)
	}

	// Initialize logging.
	utc := !cfg.LocalLogs
	if cfg.Net == dex.Simnet {
		utc = false
	}
	logMaker := initLogging(cfg.DebugLevel, utc)
	defer closeFileLogger()
	log = logMaker.Logger("DEXC")
	if utc {
		log.Infof("Logging with UTC time stamps. Current local time is %v",
			time.Now().Local().Format("15:04:05 MST"))
	}

	if !cfg.NoWeb && !cfg.NoWindow && processServerRunning() {
		log.Warn("An instance of dexc is already running")
		os.Exit(1)
	}

	// Prepare the Core.
	clientCore, err := core.New(&core.Config{
		DBPath: cfg.DBPath, // global set in config.go
		Net:    cfg.Net,
		Logger: logMaker.Logger("CORE"),
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
				closeApp()
				return
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		clientCore.Run(appCtx)
		closeApp() // in the event that Run returns prematurely prior to context cancellation
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
			closeApp()
			return
		}
		cm := dex.NewConnectionMaster(rpcSrv)
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = cm.Connect(appCtx)
			if err != nil {
				log.Errorf("Error starting rpc server: %v", err)
				closeApp()
				return
			}
			cm.Wait()
		}()
	}

	if !cfg.NoWeb {
		webSrv, err := webserver.New(clientCore, cfg.WebAddr, logMaker.Logger("WEB"), cfg.ReloadHTML)
		if err != nil {
			log.Errorf("Error creating web server: %v", err)
			closeApp()
			goto done
		}
		cm := dex.NewConnectionMaster(webSrv)
		wg.Add(1)
		go func() {
			defer wg.Done()
			err = cm.Connect(appCtx)
			if err != nil {
				log.Errorf("Error starting web server: %v", err)
				closeApp()
				return
			}
			cm.Wait()
		}()

		if !cfg.NoWindow {
			processChan, err := runProcessServer()
			if err != nil {
				log.Errorf("Error starting process server: %v", err)
				closeApp()
				return
			}
			go runUI(cfg, processChan, clientCore)
		}
	}
done:

	wg.Wait()
	log.Info("Exiting dexc main.")
}
