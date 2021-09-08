// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

/*
The LoadBot is a load tester for dcrdex. LoadBot works by running one or more
Trader routines, each with their own *core.Core (actually, a *Mantle) and
wallets.
*/

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	_ "decred.org/dcrdex/client/asset/btc"
	_ "decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/config"
	dexsrv "decred.org/dcrdex/server/dex"
	toxiproxy "github.com/Shopify/toxiproxy/client"
)

const (
	dcrBtcMarket     = "dcr_btc"
	defaultBtcPerDcr = 0.000878
	startPort        = 34560
	alpha            = "alpha"
	beta             = "beta"
	btc              = "btc"
	dcr              = "dcr"
	maxOrderLots     = 10
)

var (
	dcrID, _ = dex.BipSymbolID("dcr")
	btcID, _ = dex.BipSymbolID("btc")
	// ltcID, _    = dex.BipSymbolID("ltc") // TODO
	loggerMaker *dex.LoggerMaker
	hostAddr    = "127.0.0.1:17273"
	pass        = []byte("abc")
	log         dex.Logger
	unbip       = dex.BipIDSymbol
	portCounter uint32

	usr, _     = user.Current()
	dextestDir = filepath.Join(usr.HomeDir, "dextest")
	botDir     = filepath.Join(dextestDir, "loadbot")

	defaultRegFee   uint64 = 1e8
	defaultRegAsset uint32 = dcrID
	ctx, quit              = context.WithCancel(context.Background())

	alphaAddrDCR, betaAddrDCR, alphaAddrBTC, betaAddrBTC string
	alphaCfgDCR, betaCfgDCR,
	alphaCfgBTC, betaCfgBTC map[string]string

	dcrAssetCfg, btcAssetCfg   *dexsrv.AssetConf
	orderCounter, matchCounter uint32
	epochDuration              uint64
	lotSize                    uint64
	rateStep                   uint64
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// rpcAddr is the RPC address needed for creation of a wallet connected to the
// specified asset node. For Decred, this is the dcrwallet 'rpclisten'
// configuration parameter.For Bitcoin, its the 'rpcport' parameter.
func rpcAddr(symbol, node string) string {
	switch symbol {
	case dcr:
		switch node {
		case alpha:
			return alphaCfgDCR["rpclisten"]
		case beta:
			return betaCfgDCR["rpclisten"]
		}
	case btc:
		switch node {
		case alpha:
			return alphaCfgBTC["rpcport"]
		case beta:
			return betaCfgBTC["rpcport"]
		}
	}
	panic("rpcAddr: unknown symbol-node combo " + symbol + "-" + node)
}

// returnAddress is an address for the specified node's wallet. returnAddress
// is used when a wallet accumulates more than the max allowed for some asset.
//
func returnAddress(symbol, node string) string {
	switch symbol {
	case dcr:
		switch node {
		case alpha:
			return alphaAddrDCR
		case beta:
			return betaAddrDCR
		}
	case btc:
		switch node {
		case alpha:
			return alphaAddrBTC
		case beta:
			return betaAddrBTC
		}
	}
	panic("returnAddress: unknown symbol-node combo " + symbol + "-" + node)
}

// mineAlpha will mine a single block on the alpha node of the asset indicated
// by symbol.
func mineAlpha(symbol string) <-chan *harnessResult {
	return harnessCtl(symbol, "./mine-alpha", "1")
}

// mineBeta will mine a single block on the beta node of the asset indicated
// by symbol.
func mineBeta(symbol string) <-chan *harnessResult {
	return harnessCtl(symbol, "./mine-beta", "1")
}

// harnessResult is the result of a harnessCtl command.
type harnessResult struct {
	err    error
	output string
	cmd    string
}

// String returns a result string for the harnessResult.
func (res *harnessResult) String() string {
	if res.err != nil {
		return fmt.Sprintf("error running harness command %q: %v", res.cmd, res.err)
	}
	return fmt.Sprintf("response from harness command %q: %s", res.cmd, res.output)
}

// harnessCtl will run the command from the harness-ctl directory for the
// specified symbol.
func harnessCtl(symbol, cmd string, args ...string) <-chan *harnessResult {
	dir := filepath.Join(dextestDir, symbol, "harness-ctl")
	c := make(chan *harnessResult)
	go func() {
		fullCmd := strings.Join(append([]string{cmd}, args...), " ")
		command := exec.CommandContext(ctx, cmd, args...)
		command.Dir = dir
		output, err := command.Output()
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				log.Errorf("exec error (%s) %q: %v: %s", symbol, cmd, err, string(exitErr.Stderr))
			} else {
				log.Errorf("%s harnessCtl error running %q: %v", symbol, cmd, err)
			}
		}
		c <- &harnessResult{
			err:    err,
			output: strings.TrimSpace(string(output)),
			cmd:    fullCmd,
		}
	}()
	return c
}

// nextAddr returns a new address, as well as the selected port, both as
// strings.
func nextAddr() (string, string) {
	port := strconv.Itoa(int(atomic.AddUint32(&portCounter, 1) + startPort))
	return "127.0.0.1:" + port, port
}

func main() {
	var programName string
	var debug, trace, latency500, shaky500, slow100, spotty20 bool
	var m, n int
	flag.StringVar(&programName, "p", "", "the bot program to run")
	flag.BoolVar(&latency500, "latency500", false, "add 500 ms of latency to downstream comms for wallets and server")
	flag.BoolVar(&shaky500, "shaky500", false, "add 500 ms +/- 500ms of latency to downstream comms for wallets and server")
	flag.BoolVar(&slow100, "slow100", false, "limit bandwidth on all server and wallet connections to 100 kB/s")
	flag.BoolVar(&spotty20, "spotty20", false, "drop all connections every 20 seconds")
	flag.BoolVar(&debug, "debug", false, "use debug logging")
	flag.BoolVar(&trace, "trace", false, "use trace logging")
	flag.IntVar(&m, "m", 0, "for compound and sidestacker, m is the number of makers to stack before placing takers")
	flag.IntVar(&n, "n", 0, "for compound and sidestacker, n is the number of orders to place per epoch (default 3)")
	flag.Parse()

	if programName == "" {
		fmt.Println("no program set. use `-p program-name` to set the bot's program")
		return
	}

	// Parse the default log level and create initialize logging.
	logLevel := "info"
	if debug {
		logLevel = "debug"
	}
	if trace {
		logLevel = "trace"
	}

	// Get the server harness configuration file. We'll want these parameters
	// before we get them from 'config'.
	f, err := os.ReadFile(filepath.Join(dextestDir, "dcrdex", "markets.json"))
	if err != nil {
		fmt.Println("error reading simnet dcrdex markets.json file:", err)
		return
	}
	var mktsCfg *marketsDotJSON
	err = json.Unmarshal(f, &mktsCfg)
	if err != nil {
		fmt.Println("error unmarshaling markets.json:", err)
		return
	}
	for _, mkt := range mktsCfg.Markets {
		if mkt.Base == "DCR_simnet" && mkt.Quote == "BTC_simnet" {
			epochDuration = mkt.Duration
			lotSize = mkt.LotSize
			rateStep = mkt.RateStep
		}
	}
	if epochDuration == 0 {
		fmt.Println("failed to find dcr_btc market in harness config")
		return
	}

	btcAssetCfg = mktsCfg.Assets["BTC_simnet"]
	dcrAssetCfg = mktsCfg.Assets["DCR_simnet"]
	if dcrAssetCfg == nil || btcAssetCfg == nil {
		fmt.Println("asset configuration missing from markets.json")
		return
	}

	// Load the asset node configs. We'll need the wallet RPC addresses.
	// Note that the RPC address may be modified if toxiproxy is used below.
	alphaCfgDCR = loadNodeConfig(dcr, alpha)
	betaCfgDCR = loadNodeConfig(dcr, beta)
	alphaCfgBTC = loadNodeConfig(btc, alpha)
	betaCfgBTC = loadNodeConfig(btc, beta)

	loggerMaker, err = dex.NewLoggerMaker(os.Stdout, logLevel)
	if err != nil {
		fmt.Println("error creating LoggerMaker:", err)
	}
	log /* global */ = loggerMaker.NewLogger("LOADBOT", dex.LevelInfo)

	log.Infof("Running program %s", programName)

	getAddress := func(symbol, cmd string, args ...string) string {
		res := <-harnessCtl(symbol, cmd, args...)
		if res.err != nil {
			log.Errorf("error getting %s address: %v", symbol, res.err)
		}
		return res.output
	}

	alphaAddrDCR = getAddress(dcr, "./alpha", "getnewaddress", "default", "ignore")
	betaAddrDCR = getAddress(dcr, "./beta", "getnewaddress", "default", "ignore")
	alphaAddrBTC = getAddress(btc, "./alpha", "getnewaddress", "''", "bech32")
	betaAddrBTC = getAddress(btc, "./beta", "getnewaddress", "''", "bech32")

	// Unlock wallets, since they may have been locked on a previous shutdown.
	<-harnessCtl(dcr, "./alpha", "walletpassphrase", "abc", "0")
	<-harnessCtl(dcr, "./beta", "walletpassphrase", "abc", "0") // creating new accounts requires wallet unlocked
	<-harnessCtl(dcr, "./beta", "unlockaccount", "default", "abc")
	<-harnessCtl(btc, "./alpha", "walletpassphrase", "abc", "4294967295")
	<-harnessCtl(btc, "./beta", "walletpassphrase", "abc", "4294967295")

	// Clean up the directory.
	os.RemoveAll(botDir)
	err = os.MkdirAll(botDir, 0700)
	if err != nil {
		log.Errorf("error creating loadbot directory: %v", err)
		return
	}

	// Catch ctrl+c for a clean shutdown.
	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		quit()
	}()

	// Run any specified network conditions.
	var toxics toxiproxy.Toxics
	if latency500 {
		toxics = append(toxics, toxiproxy.Toxic{
			Name:     "latency-500",
			Type:     "latency",
			Toxicity: 1,
			Attributes: toxiproxy.Attributes{
				"latency": 500,
			},
		})
	}
	if shaky500 {
		toxics = append(toxics, toxiproxy.Toxic{
			Name:     "shaky-500",
			Type:     "latency",
			Toxicity: 1,
			Attributes: toxiproxy.Attributes{
				"latency": 500,
				"jitter":  500,
			},
		})
	}

	if slow100 {
		toxics = append(toxics, toxiproxy.Toxic{
			Name:     "slow-100",
			Type:     "bandwidth",
			Toxicity: 1,
			Attributes: toxiproxy.Attributes{
				"rate": 100,
			},
		})
	}

	if spotty20 {
		toxics = append(toxics, toxiproxy.Toxic{
			Name:     "spotty-20",
			Type:     "timeout",
			Toxicity: 1,
			Attributes: toxiproxy.Attributes{
				"timeout": 20_000,
			},
		})
	}

	if len(toxics) > 0 {
		toxiClient := toxiproxy.NewClient(":8474") // Default toxiproxy address

		pairs := []*struct {
			assetID uint32
			cfg     map[string]string
		}{
			{dcrID, alphaCfgDCR},
			{dcrID, betaCfgDCR},
			{btcID, alphaCfgBTC},
			{btcID, betaCfgBTC},
		}

		for _, pair := range pairs {
			var walletAddr, newAddr string
			newAddr, port := nextAddr()
			switch pair.assetID {
			case dcrID:
				walletAddr = pair.cfg["rpclisten"]
				pair.cfg["rpclisten"] = newAddr
			case btcID:
				oldPort := pair.cfg["rpcport"]
				walletAddr = "127.0.0.1:" + oldPort
				pair.cfg["rpcport"] = port
			}
			for _, toxic := range toxics {
				name := fmt.Sprintf("%s_%d_%s", toxic.Name, pair.assetID, port)
				proxy, err := toxiClient.CreateProxy(name, newAddr, walletAddr)
				if err != nil {
					log.Errorf("failed to create %s proxy: %v", toxic.Name, err)
					return
				}
				defer proxy.Delete()
			}
		}

		for _, toxic := range toxics {
			log.Infof("Adding network condition %s", toxic.Name)
			newAddr, _ := nextAddr()
			proxy, err := toxiClient.CreateProxy("dcrdex_"+toxic.Name, newAddr, hostAddr)
			if err != nil {
				log.Errorf("failed to create %s proxy for host: %v", toxic.Name, err)
				return
			}
			hostAddr = newAddr
			defer proxy.Delete()
		}
	}

	tStart := time.Now()

	// Run the specified program.
	switch programName {
	case "pingpong", "pingpong1":
		runPingPong(1)
	case "pingpong2":
		runPingPong(2)
	case "pingpong3":
		runPingPong(3)
	case "pingpong4":
		runPingPong(4)
	case "sidestacker":
		runSideStacker(5, 3)
	case "compound":
		runCompound()
	case "heavy":
		runHeavy()
	default:
		log.Criticalf("program " + programName + " not known")
	}

	if orderCounter > 0 {
		since := time.Since(tStart)
		rate := float64(matchCounter) / (float64(since) / float64(time.Minute))
		log.Infof("LoadBot ran for %s, during which time %d orders were placed, resulting in %d separate matches, a rate of %.3g matches / minute",
			since, orderCounter, matchCounter, rate)
	}
}

// marketsDotJSON models the server's markets.json configuration file.
type marketsDotJSON struct {
	Markets []*struct {
		Base     string  `json:"base"`
		Quote    string  `json:"quote"`
		LotSize  uint64  `json:"lotSize"`
		RateStep uint64  `json:"rateStep"`
		Duration uint64  `json:"epochDuration"`
		MBBuffer float64 `json:"marketBuyBuffer"`
	} `json:"markets"`
	Assets map[string]*dexsrv.AssetConf `json:"assets"`
}

// loadNodeConfig loads the INI configuration for the specified node into a
// map[string]string.
func loadNodeConfig(symbol, node string) map[string]string {
	cfgPath := filepath.Join(dextestDir, symbol, node, node+".conf")
	cfg, err := config.Parse(cfgPath)
	if err != nil {
		panic(fmt.Sprintf("error parsing harness config file at %s: %v", cfgPath, err))
	}
	return cfg
}
