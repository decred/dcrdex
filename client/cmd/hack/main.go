// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "decred.org/dcrdex/client/asset/btc" // register btc asset
	_ "decred.org/dcrdex/client/asset/dcr" // register dcr asset
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/rpcserver"
	"decred.org/dcrdex/client/webserver"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/msgjson"
	"decred.org/dcrdex/dex/ws"
	"github.com/decred/slog"
)

var log slog.Logger

func main() {
	err := mainCore()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	os.Exit(0)
}

var (
	walletConfs = map[uint32]*struct {
		conf, acct, pw string
	}{
		0: {
			conf: cleanAndExpandPath("~/.dexhack/btc-testnet-wallet.conf"),
			acct: "",
		},
		//2:  {conf: cleanAndExpandPath("~/.dexhack/ltc-testnet-wallet.conf")},
		42: {
			conf: cleanAndExpandPath("~/.dexhack/dcr-testnet-wallet.conf"),
			acct: "default",
		},
	}
)

func openWallet(assetID uint32, conf, walletPW, walletAccount, appPW string, clientCore *core.Core) error {
	var created, opened bool
checkYoSelf:
	if state := clientCore.WalletState(assetID); state == nil {
		if created {
			return fmt.Errorf("we already tried creating the wallet!")
		}
		log.Infof("No %s wallet found. Creating...", dex.BipIDSymbol(assetID))
		// no DCR wallet
		err := clientCore.CreateWallet(appPW, walletPW, &core.WalletForm{
			AssetID: assetID,
			Account: walletAccount,
			INIPath: conf,
		})
		if err != nil {
			return fmt.Errorf("CreateWallet error: %v", err)
		}
		created = true
		err = clientCore.OpenWallet(assetID, appPW)
		if err != nil {
			return fmt.Errorf("OpenWallet error: %v", err)
		}
		goto checkYoSelf
	} else if !state.Running || !state.Open {
		if opened {
			return fmt.Errorf("we already tried opening the wallet!")
		}
		err := clientCore.OpenWallet(assetID, appPW)
		if err != nil {
			return fmt.Errorf("OpenWallet error: %v", err)
		}
		opened = true
		goto checkYoSelf
	}
	return nil
}

func mainCore() error {
	var clientPW, dexHostPort string
	flag.StringVar(&clientPW, "appPass", "asdf", "Application password")
	flag.StringVar(&dexHostPort, "dex", "http://dex-test.ssgen.io:7232", "dex url") // why proto?

	flag.StringVar(&walletConfs[42].pw, "dcrPW", "pass", "DCR wallet password. Only needed during creation.")
	flag.StringVar(&walletConfs[0].pw, "btcPW", "pass", "BTC wallet password. Only needed during creation.")
	//flag.StringVar(&ltcWalletPW, "ltcPW", "pass", "LTC wallet password. Only needed during creation.")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		cancel()
	}()

	// Group the config items.
	cfg := struct {
		DBPath string
		Certs  map[string]string
		Net    dex.Network
		// RPC server settings
		RPCAddr, RPCUser, RPCPass, RPCCert, RPCKey string
		// Web server settings
		WebAddr string
	}{
		DBPath: "./hack.db",
		Certs: map[string]string{
			dexHostPort: "dex-test.ssgen.io.cert",
		},
		Net:     dex.Testnet,
		RPCAddr: "127.0.0.1:9292",
		RPCUser: "u",
		RPCPass: "p",
		RPCCert: "hack.cert",
		RPCKey:  "hack.key",
		WebAddr: "127.0.0.1:9293",
	}

	stdoutBackend := slog.NewBackend(os.Stdout)
	logMaker, err := dex.NewLoggerMaker(stdoutBackend, "trace")
	if err != nil {
		return err
	}
	log = logMaker.Logger("HACK")
	ws.UseLogger(logMaker.Logger("WS"))
	comms.UseLogger(logMaker.Logger("COMMS"))

	clientCore, err := core.New(&core.Config{
		DBPath:      cfg.DBPath, // global set in config.go
		LoggerMaker: logMaker,
		Certs:       cfg.Certs,
		Net:         cfg.Net,
	})
	if err != nil {
		return fmt.Errorf("error creating client core: %v", err)
	}

	var wg sync.WaitGroup
	coreRunner := dex.NewStartStopWaiter(clientCore)
	coreRunner.Start(context.Background())

	time.Sleep(1 * time.Second) // TODO: get rid of this

	// Start the RPC and web servers in case we want to use them to debug
	// something, but this tool is primarily for scripting certain actions.
	wg.Add(1)
	go func() {
		defer wg.Done()
		rpcserver.SetLogger(log)
		rpcCfg := &rpcserver.Config{
			Core: clientCore,
			Addr: cfg.RPCAddr,
			User: cfg.RPCUser,
			Pass: cfg.RPCPass,
			Cert: cfg.RPCCert,
			Key:  cfg.RPCKey}
		rpcSrv, err := rpcserver.New(rpcCfg)
		if err != nil {
			log.Errorf("Error starting rpc server: %v", err)
			cancel()
			return
		}
		rpcSrv.Run(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		webSrv, err := webserver.New(clientCore, cfg.WebAddr, logMaker.Logger("WEBS"), true)
		if err != nil {
			log.Errorf("Error starting web server: %v", err)
			cancel()
			return
		}
		webSrv.Run(ctx)
	}()

	shutItDown := func() {
		cancel()
		wg.Wait()
		coreRunner.Stop()
		coreRunner.WaitForShutdown()
	}

	// Get the client set up.
	user := clientCore.User()
	if user == nil || !user.Initialized {
		if user == nil {
			log.Debugf("No user!")
		} else {
			log.Debugf("No initialized user!")
		}
		err = clientCore.InitializeClient(clientPW)
		if err != nil {
			shutItDown()
			return fmt.Errorf("InitializeClient error: %v", err)
		}
	}

	_, err = clientCore.Login(clientPW)
	if err != nil {
		shutItDown()
		return fmt.Errorf("Login error: %v", err)
	}

	user = clientCore.User() // needed again?
	_, alreadyRegistered := user.Markets[dexHostPort]

	var regFee uint64
	if !alreadyRegistered {
		regFee, err = clientCore.PreRegister(dexHostPort)
		if err != nil {
			shutItDown()
			return fmt.Errorf("PreRegister error: %v", err)
		}
		log.Infof("Connected to dex %s, requiring fee %d", dexHostPort, regFee)
	}

	enumWallets := func() {
		// clientCore.SupportedAssets first
		// also clientCore.Balance
		wallets := clientCore.Wallets()
		log.Infof("Found %d wallet(s).", len(wallets))
		for _, w := range wallets {
			log.Infof("-Wallet %s (%d): running=%v, open=%v, balance=%d",
				w.Symbol, w.AssetID, w.Running, w.Open, w.Balance)
		}
	}
	enumWallets()

	// Open or create all wallets listed in walletConfs.
	for assetID, conf := range walletConfs {
		err = openWallet(assetID, conf.conf, conf.pw, conf.acct, clientPW, clientCore)
		if err != nil {
			shutItDown()
			return err
		}
	}

	enumWallets()

	if !alreadyRegistered {
		log.Infof("Trying to register at %s with a fee of %f DCR", dexHostPort, float64(regFee)/1e8)
		err, payFeeErr := clientCore.Register(&core.Registration{
			DEX:      dexHostPort,
			Password: clientPW,
			Fee:      regFee,
		})
		if err != nil {
			shutItDown()
			return fmt.Errorf("Register error: %v", err)
		}

		log.Infof("Waiting for fee payment...")
		select {
		case err = <-payFeeErr:
		case <-ctx.Done():
			shutItDown()
			return fmt.Errorf("interrupted")
		}
		if err != nil {
			shutItDown()
			return fmt.Errorf("Register payFeeErr: %v", err)
		}
		log.Info("It worked!")
	}

	dexMkts := clientCore.Markets()[dexHostPort]
	log.Infof("%s markets: ", dexHostPort)
	for _, mkt := range dexMkts {
		log.Infof("- %s(%d)-%s(%d)", mkt.BaseSymbol, mkt.BaseID, mkt.QuoteSymbol, mkt.QuoteID)
	}

	conn, _ /*aid*/, _ /*dexPk*/, _ /*signer*/ := clientCore.DEXConn(dexHostPort)
	if conn == nil {
		shutItDown()
		return fmt.Errorf("failed to retrieve DEX WsConn")
	}

	msgID := conn.NextID()
	route := "config"
	payload := "asdf"
	msg, err := msgjson.NewRequest(msgID, route, payload)
	if err != nil {
		shutItDown()
		return fmt.Errorf("NewRequest failed: %v", err)
	}
	conn.Request(msg, func(resp *msgjson.Message) {
		//res := make(map[string]interface{})
		var res json.RawMessage
		err = resp.UnmarshalResult(&res)
		if err != nil {
			log.Error(err)
			if len(res) == 0 {
				return // res should be empty
			}
		}

		var resIndented bytes.Buffer
		err = json.Indent(&resIndented, res, "", "    ")
		if err == nil {
			log.Infof("response to request %d (%s):\n%v", msgID, route, resIndented.String())
		} else {
			log.Infof("raw response to request %d (%s):\n%v", msgID, route, string(res))
		}
	})

	// book := clientCore.Book(dexHostPort, 42, 0)
	// if book != nil {
	// 	log.Infof("Retrieved book for %s with %d buys, %d sells.", dexHostPort,
	// 		len(book.Buys), len(book.Sells))
	// }

	wg.Wait()
	coreRunner.Stop()
	coreRunner.WaitForShutdown()
	return nil
}

// cleanAndExpandPath expands environment variables and leading ~ in the passed
// path, cleans the result, and returns it.
func cleanAndExpandPath(path string) string {
	// NOTE: The os.ExpandEnv doesn't work with Windows cmd.exe-style
	// %VARIABLE%, but the variables can still be expanded via POSIX-style
	// $VARIABLE.
	path = os.ExpandEnv(path)

	if !strings.HasPrefix(path, "~") {
		return filepath.Clean(path)
	}

	// Expand initial ~ to the current user's home directory, or ~otheruser to
	// otheruser's home directory.  On Windows, both forward and backward
	// slashes can be used.
	path = path[1:]

	var pathSeparators string
	if runtime.GOOS == "windows" {
		pathSeparators = string(os.PathSeparator) + "/"
	} else {
		pathSeparators = string(os.PathSeparator)
	}

	userName := ""
	if i := strings.IndexAny(path, pathSeparators); i != -1 {
		userName = path[:i]
		path = path[i:]
	}

	homeDir := ""
	var u *user.User
	var err error
	if userName == "" {
		u, err = user.Current()
	} else {
		u, err = user.Lookup(userName)
	}
	if err == nil {
		homeDir = u.HomeDir
	}
	// Fallback to CWD if user lookup fails or user has no home directory.
	if homeDir == "" {
		homeDir = "."
	}

	return filepath.Join(homeDir, path)
}
