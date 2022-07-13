// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	_ "decred.org/dcrdex/client/asset/bch"  // register bch asset
	_ "decred.org/dcrdex/client/asset/btc"  // register btc asset
	_ "decred.org/dcrdex/client/asset/dcr"  // register dcr asset
	_ "decred.org/dcrdex/client/asset/doge" // register doge asset
	_ "decred.org/dcrdex/client/asset/ltc"  // register ltc asset
	_ "decred.org/dcrdex/client/asset/zec"  // register zec asset

	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/client/webserver"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/version"
)

func main() {
	// Wrap the actual main so defers run in it.
	err := mainCore()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func mainCore() error {
	appCtx, cancel := context.WithCancel(context.Background())
	defer cancel() // don't leak on the earliest returns

	// Parse configuration.
	cfg, err := configure()
	if err != nil {
		return fmt.Errorf("configration error: %w", err)
	}

	// If explicitly running without web server then you must run the rpc
	// server.
	if cfg.NoWeb && !cfg.RPCOn {
		return fmt.Errorf("cannot run without web server unless --rpc is specified")
	}

	if cfg.CPUProfile != "" {
		var f *os.File
		f, err = os.Create(cfg.CPUProfile)
		if err != nil {
			return fmt.Errorf("error starting CPU profiler: %w", err)
		}
		err = pprof.StartCPUProfile(f)
		if err != nil {
			return fmt.Errorf("error starting CPU profiler: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	// Initialize logging.
	utc := !cfg.LocalLogs
	if cfg.Net == dex.Simnet {
		utc = false
	}
	logMaker := initLogging(cfg.DebugLevel, utc)
	log = logMaker.Logger("DEXC")
	log.Infof("%s version %v (Go version %s)", appName, Version, runtime.Version())
	if utc {
		log.Infof("Logging with UTC time stamps. Current local time is %v",
			time.Now().Local().Format("15:04:05 MST"))
	}
	log.Infof("dexc starting for network: %s", cfg.Net)
	log.Infof("Swap locktimes config: maker %s, taker %s",
		dex.LockTimeMaker(cfg.Net), dex.LockTimeTaker(cfg.Net))

	defer func() {
		if pv := recover(); pv != nil {
			log.Criticalf("Uh-oh! \n\nPanic:\n\n%v\n\nStack:\n\n%v\n\n",
				pv, string(debug.Stack()))
		}
		closeFileLogger()
	}()

	// Prepare the Core.
	clientCore, err := core.New(&core.Config{
		DBPath:       cfg.DBPath, // global set in config.go
		Net:          cfg.Net,
		Logger:       logMaker.Logger("CORE"),
		TorProxy:     cfg.TorProxy,
		TorIsolation: cfg.TorIsolation,
		Onion:        cfg.Onion,
		Language:     cfg.Language,
	})
	if err != nil {
		return fmt.Errorf("error creating client core: %w", err)
	}

	// Catch interrupt signal (e.g. ctrl+c), prompting to shutdown if the user
	// is logged in, and there are active orders or matches.
	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		for range killChan {
			if promptShutdown(clientCore) {
				log.Infof("Shutting down...")
				cancel()
				return
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		clientCore.Run(appCtx)
		cancel() // in the event that Run returns prematurely prior to context cancellation
	}()

	<-clientCore.Ready()

	defer func() {
		log.Info("Exiting dexc main.")
		cancel()  // no-op with clean rpc/web server setup
		wg.Wait() // no-op with clean setup and shutdown
	}()

	if cfg.RPCOn {
		// Prepare dexc version for use with rpc server.
		dexcMajor, dexcMinor, dexcPatch, dexcPreRel, dexcBuildMeta, err := version.ParseSemVer(Version)
		if err != nil {
			return fmt.Errorf("failed to parse %s version: %w", appName, err)
		}

		runtimeVer := strings.Replace(runtime.Version(), ".", "-", -1)
		runBuildMeta := version.NormalizeString(runtimeVer)
		build := version.NormalizeString(dexcBuildMeta)
		if build != "" {
			dexcBuildMeta = fmt.Sprintf("%s.%s", build, runBuildMeta)
		}
		dexcVersion := &rpcserver.SemVersion{
			VersionString: Version,
			Major:         dexcMajor,
			Minor:         dexcMinor,
			Patch:         dexcPatch,
			Prerelease:    dexcPreRel,
			BuildMetadata: dexcBuildMeta,
		}

		rpcserver.SetLogger(logMaker.Logger("RPC"))
		rpcCfg := &rpcserver.Config{
			Core:        clientCore,
			Addr:        cfg.RPCAddr,
			User:        cfg.RPCUser,
			Pass:        cfg.RPCPass,
			Cert:        cfg.RPCCert,
			Key:         cfg.RPCKey,
			DexcVersion: dexcVersion,
			CertHosts:   cfg.CertHosts,
		}
		rpcSrv, err := rpcserver.New(rpcCfg)
		if err != nil {
			return fmt.Errorf("failed to create rpc server: %w", err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			cm := dex.NewConnectionMaster(rpcSrv)
			err := cm.Connect(appCtx)
			if err != nil {
				log.Errorf("Error starting rpc server: %v", err)
				cancel()
				return
			}
			cm.Wait()
		}()
	}

	if !cfg.NoWeb {
		webSrv, err := webserver.New(&webserver.Config{
			Core:          clientCore,
			Addr:          cfg.WebAddr,
			CustomSiteDir: cfg.SiteDir,
			Logger:        logMaker.Logger("WEB"),
			NoEmbed:       cfg.NoEmbedSite,
			HttpProf:      cfg.HTTPProfile,
			Language:      cfg.Language,
		})
		if err != nil {
			return fmt.Errorf("failed creating web server: %w", err)
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			cm := dex.NewConnectionMaster(webSrv)
			err := cm.Connect(appCtx)
			if err != nil {
				log.Errorf("Error starting web server: %v", err)
				cancel()
				return
			}
			cm.Wait()
		}()
	}

	// Wait for everything to stop.
	wg.Wait()

	return nil
}

// promptShutdown checks if there are active orders and asks confirmation to
// shutdown if there are. The return value indicates if it is safe to stop Core
// or if the user has confirmed they want to shutdown with active orders.
func promptShutdown(clientCore *core.Core) bool {
	log.Infof("Attempting to logout...")
	// Do not allow Logout hanging to prevent shutdown.
	res := make(chan bool, 1)
	go func() {
		// Only block logout if err is ActiveOrdersLogoutErr.
		var ok bool
		err := clientCore.Logout()
		if err == nil {
			ok = true
		} else if !errors.Is(err, core.ActiveOrdersLogoutErr) {
			log.Errorf("Unexpected logout error: %v", err)
			ok = true
		} // else not ok => prompt
		res <- ok
	}()

	select {
	case <-time.After(10 * time.Second):
		log.Errorf("Timeout waiting for Logout. Allowing shutdown, but you likely have active orders!")
		return true // cancel all the contexts, hopefully breaking whatever deadlock
	case ok := <-res:
		if ok {
			return true
		}
	}

	fmt.Print("You have active orders. Shutting down now may result in failed swaps and account penalization.\n" +
		"Do you want to quit anyway? ('yes' to quit, or enter to abort shutdown): ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan() // waiting for user input
	if err := scanner.Err(); err != nil {
		fmt.Printf("Input error: %v", err)
		return false
	}

	switch resp := strings.ToLower(scanner.Text()); resp {
	case "y", "yes":
		return true
	case "n", "no", "":
	default: // anything else aborts, but warn about it
		fmt.Printf("Unrecognized response %q. ", resp)
	}
	fmt.Println("Shutdown aborted.")
	return false
}
