// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/server/admin"
	_ "decred.org/dcrdex/server/asset/btc" // register btc asset
	_ "decred.org/dcrdex/server/asset/dcr" // register dcr asset
	_ "decred.org/dcrdex/server/asset/ltc" // register ltc asset
	dexsrv "decred.org/dcrdex/server/dex"
	"decred.org/dcrdex/server/swap"
	"github.com/decred/dcrd/dcrec/secp256k1/v2"
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

	// Request admin server password if admin server is enabled and
	// server password is not set in config.
	var adminSrvAuthSHA [32]byte
	if cfg.AdminSrvOn {
		if len(cfg.AdminSrvPW) == 0 {
			adminSrvAuthSHA, err = admin.PasswordHashPrompt(ctx, "Admin interface password: ")
			if err != nil {
				return fmt.Errorf("cannot use password: %v", err)
			}
		} else {
			adminSrvAuthSHA = sha256.Sum256(cfg.AdminSrvPW)
			encode.ClearBytes(cfg.AdminSrvPW)
		}
	}

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

	// Load, or create and save, the DEX signing key.
	var privKey *secp256k1.PrivateKey
	if len(cfg.SigningKeyPW) == 0 {
		cfg.SigningKeyPW, err = admin.PasswordPrompt(ctx, "Signing key password: ")
		if err != nil {
			return fmt.Errorf("cannot use password: %v", err)
		}
	}
	privKey, err = dexKey(cfg.DEXPrivKeyPath, cfg.SigningKeyPW)
	encode.ClearBytes(cfg.SigningKeyPW)
	if err != nil {
		return err
	}

	// TODO: add dcrdex flags and config options: 1) specify state file, 2) do
	// not load state, 3) load latest state without prompt.
	log.Infof("Searching for swap state files in %q", cfg.DataDir)
	stateFile, err := swap.LatestStateFile(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("unable to read datadir: %v", err)
	}
	var state *swap.State
	if stateFile != nil {
		fmt.Printf("Load swapper state from file %q with time stamp %v? (y, n, or enter to abort) ",
			stateFile.Name, encode.UnixTimeMilli(stateFile.Stamp))
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		promptResp := scanner.Text()
		err = scanner.Err()
		if err != nil {
			return fmt.Errorf("input failed: %v", err)
		}
		// reader := bufio.NewReader(os.Stdin)
		// promptResp, err := reader.ReadString('\n')
		// if err != nil {
		// 	return fmt.Errorf("aborted input: %v", err)
		// }

		var doLoad bool
		switch strings.ToLower(promptResp) {
		case "y", "yes":
			doLoad = true
		case "n", "no":
		case "":
			return errors.New("input aborted")
		default:
			return fmt.Errorf("invalid response: %q", promptResp)
		}

		if doLoad {
			state, err = swap.LoadStateFile(stateFile.Name)
			if err != nil {
				return fmt.Errorf("failed to load swap state file %v: %v", stateFile.Name, err)
			}
			log.Infof("Loaded swap state file %q, containing %d live matches with "+
				"%d pending client acks and %d live coin waiters", stateFile.Name,
				len(state.MatchTrackers), len(state.LiveAckers), len(state.LiveWaiters))
		}
	} else {
		log.Info("No swap state files found.")
	}

	// Create the DEX manager.
	dexConf := &dexsrv.DexConf{
		SwapState:  state,
		DataDir:    cfg.DataDir,
		LogBackend: cfg.LogMaker,
		Markets:    markets,
		Assets:     assets,
		Network:    cfg.Network,
		DBConf: &dexsrv.DBConf{
			DBName:       cfg.DBName,
			Host:         cfg.DBHost,
			User:         cfg.DBUser,
			Port:         cfg.DBPort,
			Pass:         cfg.DBPass,
			ShowPGConfig: cfg.ShowPGConfig,
		},
		RegFeeXPub:       cfg.RegFeeXPub,
		RegFeeAmount:     cfg.RegFeeAmount,
		RegFeeConfirms:   cfg.RegFeeConfirms,
		BroadcastTimeout: cfg.BroadcastTimeout,
		CancelThreshold:  cfg.CancelThreshold,
		Anarchy:          cfg.Anarchy,
		DEXPrivKey:       privKey,
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

	var wg sync.WaitGroup
	if cfg.AdminSrvOn {
		srvCFG := &admin.SrvConfig{
			Core:    dexMan,
			Addr:    cfg.AdminSrvAddr,
			AuthSHA: adminSrvAuthSHA,
			Cert:    cfg.RPCCert,
			Key:     cfg.RPCKey,
		}
		adminServer, err := admin.NewServer(srvCFG)
		if err != nil {
			return fmt.Errorf("cannot set up admin server: %v", err)
		}
		wg.Add(1)
		go func() {
			adminServer.Run(ctx)
			wg.Done()
		}()
	}

	log.Info("The DEX is running. Hit CTRL+C to quit...")
	<-ctx.Done()
	// Wait for the admin server to finish.
	wg.Wait()

	log.Info("Stopping DEX...")
	dexMan.Stop()
	log.Info("Bye!")

	return nil
}

func main() {
	// Create a context that is canceled when a shutdown request is received
	// via requestShutdown.
	ctx := withShutdownCancel(context.Background())
	// Listen for both interrupt signals (e.g. CTRL+C) and shutdown requests
	// (requestShutdown calls).
	go shutdownListener()

	err := mainCore(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
