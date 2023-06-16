// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

/*
The LoadBot is a load tester for dcrdex. LoadBot works by running one or more
Trader routines, each with their own *core.Core (actually, a *Mantle) and
wallets.

Build with server locktimes in mind.
i.e. -ldflags "-X 'decred.org/dcrdex/dex.testLockTimeTaker=30s' -X 'decred.org/dcrdex/dex.testLockTimeMaker=1m'"

Supported assets are bch, btc, dcr, doge, dgb, eth, firo, ltc, and zec.
*/

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"decred.org/dcrdex/client/asset"
	_ "decred.org/dcrdex/client/asset/bch"
	_ "decred.org/dcrdex/client/asset/btc"
	_ "decred.org/dcrdex/client/asset/dcr"
	_ "decred.org/dcrdex/client/asset/dgb"
	_ "decred.org/dcrdex/client/asset/doge"
	_ "decred.org/dcrdex/client/asset/eth"
	_ "decred.org/dcrdex/client/asset/firo"
	_ "decred.org/dcrdex/client/asset/ltc"
	_ "decred.org/dcrdex/client/asset/zec"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/config"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	dexsrv "decred.org/dcrdex/server/dex"
	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
)

const (
	rateEncFactor    = calc.RateEncodingFactor
	defaultBtcPerDcr = 0.000878
	alpha            = "alpha"
	beta             = "beta"
	btc              = "btc"
	dcr              = "dcr"
	eth              = "eth"
	firo             = "firo"
	ltc              = "ltc"
	doge             = "doge"
	dgb              = "dgb"
	bch              = "bch"
	zec              = "zec"
	dextt            = "dextt.eth"
	maxOrderLots     = 10
	ethFeeRate       = 200 // gwei
	// missedCancelErrStr is part of an error found in dcrdex/server/market/orderrouter.go
	// that a cancel order may hit with bad timing but is not a problem.
	//
	// TODO: Consider returning a separate msgjson error from server for
	// this case.
	missedCancelErrStr = "target order not known:"
)

var (
	dcrID, _    = dex.BipSymbolID(dcr)
	btcID, _    = dex.BipSymbolID(btc)
	ethID, _    = dex.BipSymbolID(eth)
	dexttID, _  = dex.BipSymbolID(dextt)
	ltcID, _    = dex.BipSymbolID(ltc)
	dogeID, _   = dex.BipSymbolID(doge)
	dgbID, _    = dex.BipSymbolID(dgb)
	firoID, _   = dex.BipSymbolID(firo)
	bchID, _    = dex.BipSymbolID(bch)
	zecID, _    = dex.BipSymbolID(zec)
	loggerMaker *dex.LoggerMaker
	hostAddr    = "127.0.0.1:17273"
	pass        = []byte("abc")
	log         dex.Logger
	unbip       = dex.BipIDSymbol

	usr, _       = user.Current()
	dextestDir   = filepath.Join(usr.HomeDir, "dextest")
	botDir       = filepath.Join(dextestDir, fmt.Sprintf("loadbot_%d", time.Now().Unix()))
	alphaIPCFile = filepath.Join(dextestDir, "eth", "alpha", "node", "geth.ipc")
	betaIPCFile  = filepath.Join(dextestDir, "eth", "beta", "node", "geth.ipc")

	ctx, quit = context.WithCancel(context.Background())

	alphaAddrBase, betaAddrBase, alphaAddrQuote, betaAddrQuote, market, baseSymbol, quoteSymbol string
	alphaCfgBase, betaCfgBase,
	alphaCfgQuote, betaCfgQuote map[string]string

	baseAssetCfg, quoteAssetCfg                           *dexsrv.AssetConf
	orderCounter, matchCounter, baseID, quoteID, regAsset uint32
	epochDuration                                         uint64
	lotSize                                               uint64
	rateStep                                              uint64
	rateShift, rateIncrease                               int64
	conversionFactors                                     = make(map[string]uint64)

	ethInitFee                                     = (dexeth.InitGas(1, 0) + dexeth.RefundGas(0)) * ethFeeRate
	ethRedeemFee                                   = dexeth.RedeemGas(1, 0) * ethFeeRate
	defaultMidGap, marketBuyBuffer, whalePercent   float64
	keepMidGap, oscillate, randomOsc, ignoreErrors bool
	oscInterval, oscStep, whaleFrequency           uint64

	processesMtx sync.Mutex
	processes    []*process

	// zecSendMtx prevents sending funds too soon after mining a block and
	// the harness choosing spent outputs for Zcash.
	zecSendMtx sync.Mutex
)

func init() {
	rand.Seed(time.Now().UnixNano())
	dexeth.MaybeReadSimnetAddrs()
}

// process stores a long running command and the funcion to stop it on shutdown.
type process struct {
	cmd    *exec.Cmd
	stopFn func(ctx context.Context)
}

// findOpenAddrs finds unused addresses.
func findOpenAddrs(n int) ([]net.Addr, error) {
	addrs := make([]net.Addr, 0, n)
	for i := 0; i < n; i++ {
		addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
		if err != nil {
			return nil, err
		}

		l, err := net.ListenTCP("tcp", addr)
		if err != nil {
			return nil, err
		}
		defer l.Close()
		addrs = append(addrs, l.Addr())
	}

	return addrs, nil
}

// rpcAddr is the RPC address needed for creation of a wallet connected to the
// specified asset node. For Decred, this is the dcrwallet 'rpclisten'
// configuration parameter. For Bitcoin, its the 'rpcport' parameter. WARNING:
// with the "singular wallet" assets like zec and doge, this port may not be for
// fresh nodes created with the start-wallet script, just the alpha and beta
// nodes' wallets.
func rpcAddr(symbol, node string) string {
	var key string

	switch symbol {
	case dcr:
		key = "rpclisten"
	case btc, ltc, bch, zec, doge:
		key = "rpcport"
	case eth, dextt:
		key = "ListenAddr"
	}

	if symbol == baseSymbol {
		if node == alpha {
			return alphaCfgBase[key]
		}
		return betaCfgBase[key]
	}
	if node == alpha {
		return alphaCfgQuote[key]
	}
	return betaCfgQuote[key]
}

// returnAddress is an address for the specified node's wallet. returnAddress
// is used when a wallet accumulates more than the max allowed for some asset.
func returnAddress(symbol, node string) string {
	if symbol == baseSymbol {
		if node == alpha {
			return alphaAddrBase
		}
		return betaAddrBase
	}
	if node == alpha {
		return alphaAddrQuote
	}
	return betaAddrQuote
}

// mine will mine a single block on the node and asset indicated.
func mine(symbol, node string) <-chan *harnessResult {
	n := 1
	switch symbol {
	case eth:
		// geth may not include some tx at first because ???. Mine more.
		n = 4
	case zec:
		// Zcash has a problem selecting unused utxo for a second when
		// also mining. https://github.com/zcash/zcash/issues/6045
		zecSendMtx.Lock()
		defer func() {
			time.Sleep(time.Second)
			zecSendMtx.Unlock()
		}()
	}
	return harnessCtl(ctx, symbol, fmt.Sprintf("./mine-%s", node), fmt.Sprintf("%d", n))
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

func harnessSymbol(symbol string) string {
	switch symbol {
	case dextt:
		return eth
	}
	return symbol
}

// harnessCtl will run the command from the harness-ctl directory for the
// specified symbol. ctx shadows the global context. The global context is not
// used because stopping some nodes will occur after it is canceled.
func harnessCtl(ctx context.Context, symbol, cmd string, args ...string) <-chan *harnessResult {
	dir := filepath.Join(dextestDir, harnessSymbol(symbol), "harness-ctl")
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

// harnessProcessCtl will run the long running command from the harness-ctl
// directory for the specified symbol. The command and stop function are saved
// to a global slice for stopping later.
func harnessProcessCtl(symbol string, stopFn func(context.Context), cmd string, args ...string) error {
	processesMtx.Lock()
	defer processesMtx.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Join(dextestDir, harnessSymbol(symbol), "harness-ctl")
	// Killing the process with ctx or process.Kill *sometimes* does not
	// seem to stop the coin daemon. Kill later with an rpc "stop" command
	// contained in the stop function.
	command := exec.Command(cmd, args...)
	command.Dir = dir
	p := &process{
		cmd:    command,
		stopFn: stopFn,
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("unable to start process: %v", err)
	}
	processes = append(processes, p)
	return nil
}

// nextAddr returns a new address, as well as the selected port, both as
// strings.
func nextAddr() (addrPort, port string, err error) {
	addrs, err := findOpenAddrs(1)
	if err != nil {
		return "", "", err
	}
	addrPort = addrs[0].String()
	_, port, err = net.SplitHostPort(addrPort)
	if err != nil {
		return "", "", err
	}
	return addrPort, port, nil
}

// shutdown stops some long running processes.
func shutdown() {
	processesMtx.Lock()
	defer processesMtx.Unlock()
	for _, p := range processes {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
		defer cancel()
		p.stopFn(ctx)
		if err := p.cmd.Wait(); err != nil {
			fmt.Printf("failed to wait for stopped process: %v\n", err)
		}
	}
}

func main() {
	// Catch ctrl+c for a clean shutdown.
	killChan := make(chan os.Signal, 1)
	signal.Notify(killChan, os.Interrupt)
	go func() {
		<-killChan
		quit()
	}()
	if err := run(); err != nil {
		println(err.Error())
		quit()
		os.Exit(1)
	}
	quit()
	os.Exit(0)
}

func run() error {
	defer shutdown()
	var programName string
	var debug, trace, latency500, shaky500, slow100, spotty20, registerWithQuote bool
	var m, n int
	flag.StringVar(&programName, "p", "", "the bot program to run")
	flag.StringVar(&market, "mkt", "dcr_btc", "the market to run on")
	flag.BoolVar(&registerWithQuote, "rwq", false, "pay the register fee with the quote asset (default is base)")
	flag.BoolVar(&keepMidGap, "kmg", false, "disallow mid gap drift")
	flag.BoolVar(&latency500, "latency500", false, "add 500 ms of latency to downstream comms for wallets and server")
	flag.BoolVar(&shaky500, "shaky500", false, "add 500 ms +/- 500ms of latency to downstream comms for wallets and server")
	flag.BoolVar(&slow100, "slow100", false, "limit bandwidth on all server and wallet connections to 100 kB/s")
	flag.BoolVar(&spotty20, "spotty20", false, "drop all connections every 20 seconds")
	flag.BoolVar(&debug, "debug", false, "use debug logging")
	flag.BoolVar(&trace, "trace", false, "use trace logging")
	flag.IntVar(&m, "m", 0, "for compound and sidestacker, m is the number of makers to stack before placing takers")
	flag.IntVar(&n, "n", 0, "for compound and sidestacker, n is the number of orders to place per epoch (default 3)")
	flag.Int64Var(&rateShift, "rateshift", 0, "for compound and sidestacker, rateShift is applied to every order and increases or decreases price by the chosen shift times the rate step, use to create a market trending in one direction (default 0)")
	flag.BoolVar(&oscillate, "oscillate", false, "for compound and sidestacker, whether the price should move up and down inside a window, use to emulate a sideways market (default false)")
	flag.BoolVar(&randomOsc, "randomosc", false, "for compound and sidestacker, oscillate more randomly")
	flag.Uint64Var(&oscInterval, "oscinterval", 300, "for compound and sidestacker, the number of epochs to take for a full oscillation cycle")
	flag.Uint64Var(&oscStep, "oscstep", 50, "for compound and sidestacker, the number of rate step to increase or decrease per epoch")
	flag.BoolVar(&ignoreErrors, "ignoreerrors", false, "log and ignore errors rather than the default behavior of stopping loadbot")
	flag.Uint64Var(&whaleFrequency, "whalefrequency", 4, "controls the frequency with which the whale \"whales\" after it is ready. To whale is to choose a rate and attempt to buy up the entire book at that price. If frequency is N, the whale will whale an average of 1 out of every N+1 epochs (default 4)")
	flag.Float64Var(&whalePercent, "whalepercent", 0.1, "The percent of the current mid gap to whale within. If 0.1 the whale will pick a target price between 0.9 and 1.1 percent of the current mid gap (default 0.1)")
	flag.Parse()

	if programName == "" {
		return errors.New("no program set. use `-p program-name` to set the bot's program")
	}

	// Parse the default log level and create initialize logging.
	logLevel := "info"
	if debug {
		logLevel = "debug"
	}
	if trace {
		logLevel = "trace"
	}

	symbols := strings.Split(market, "_")
	if len(symbols) != 2 {
		return fmt.Errorf("invalid market %q", market)
	}
	baseSymbol = strings.ToLower(symbols[0])
	quoteSymbol = strings.ToLower(symbols[1])

	var found bool
	baseID, found = dex.BipSymbolID(baseSymbol)
	if !found {
		return fmt.Errorf("base asset %q not found", baseSymbol)
	}
	quoteID, found = dex.BipSymbolID(quoteSymbol)
	if !found {
		return fmt.Errorf("quote asset %q not found", quoteSymbol)
	}
	regAsset = baseID
	if registerWithQuote {
		regAsset = quoteID
	}
	ui, err := asset.UnitInfo(baseID)
	if err != nil {
		return fmt.Errorf("cannot get base %q unit info: %v", baseSymbol, err)
	}
	conversionFactors[baseSymbol] = ui.Conventional.ConversionFactor
	ui, err = asset.UnitInfo(quoteID)
	if err != nil {
		return fmt.Errorf("cannot get quote %q unit info: %v", quoteSymbol, err)
	}
	conversionFactors[quoteSymbol] = ui.Conventional.ConversionFactor

	f, err := os.ReadFile(filepath.Join(dextestDir, "dcrdex", "markets.json"))
	if err != nil {
		return fmt.Errorf("error reading simnet dcrdex markets.json file: %v", err)
	}
	var mktsCfg marketsDotJSON
	err = json.Unmarshal(f, &mktsCfg)
	if err != nil {
		return fmt.Errorf("error unmarshaling markets.json: %v", err)
	}

	markets := make([]string, len(mktsCfg.Markets))
	for i, mkt := range mktsCfg.Markets {
		getSymbol := func(name string) (string, error) {
			asset, ok := mktsCfg.Assets[name]
			if !ok {
				return "", fmt.Errorf("config does not have an asset that matches %s", name)
			}
			return strings.ToLower(asset.Symbol), nil
		}
		bs, err := getSymbol(mkt.Base)
		if err != nil {
			return err
		}
		qs, err := getSymbol(mkt.Quote)
		if err != nil {
			return err
		}
		markets[i] = fmt.Sprintf("%s_%s", bs, qs)
		if qs != quoteSymbol || bs != baseSymbol {
			continue
		}
		lotSize = mkt.LotSize
		rateStep = mkt.RateStep
		epochDuration = mkt.Duration
		marketBuyBuffer = mkt.MBBuffer
		break
	}

	rateIncrease = int64(rateStep) * rateShift

	// Adjust to be comparable to the dcr_btc market.
	defaultMidGap = defaultBtcPerDcr * float64(rateStep) / 100

	if epochDuration == 0 {
		return fmt.Errorf("failed to find %q market in harness config. Available markets: %s", market, strings.Join(markets, ", "))
	}

	shortSymbol := func(s string) string {
		parts := strings.Split(s, ".")
		return parts[0]
	}

	baseAssetCfg = mktsCfg.Assets[fmt.Sprintf("%s_simnet", strings.ToUpper(shortSymbol(baseSymbol)))]
	quoteAssetCfg = mktsCfg.Assets[fmt.Sprintf("%s_simnet", strings.ToUpper(shortSymbol(quoteSymbol)))]
	if baseAssetCfg == nil || quoteAssetCfg == nil {
		return errors.New("asset configuration missing from markets.json")
	}

	// Load the asset node configs. We'll need the wallet RPC addresses.
	// Note that the RPC address may be modified if toxiproxy is used below.
	alphaCfgBase = loadNodeConfig(baseSymbol, alpha)
	betaCfgBase = loadNodeConfig(baseSymbol, beta)
	alphaCfgQuote = loadNodeConfig(quoteSymbol, alpha)
	betaCfgQuote = loadNodeConfig(quoteSymbol, beta)

	loggerMaker, err = dex.NewLoggerMaker(os.Stdout, logLevel)
	if err != nil {
		return fmt.Errorf("error creating LoggerMaker: %v", err)
	}
	log /* global */ = loggerMaker.NewLogger("LOADBOT")

	log.Infof("Running program %s", programName)

	getAddress := func(symbol, node string) (string, error) {
		var args []string
		switch symbol {
		case btc, ltc, dgb:
			args = []string{"getnewaddress", "''", "bech32"}
		case doge, bch, firo, zec:
			args = []string{"getnewaddress"}
		case dcr:
			args = []string{"getnewaddress", "default", "ignore"}
		case eth, dextt:
			args = []string{"attach", `--exec eth.accounts[1]`}
		default:
			return "", fmt.Errorf("getAddress: unknown symbol %q", symbol)
		}
		res := <-harnessCtl(ctx, symbol, fmt.Sprintf("./%s", node), args...)
		if res.err != nil {
			return "", fmt.Errorf("error getting %s address: %v", symbol, res.err)
		}
		return strings.Trim(res.output, `"`), nil
	}

	if alphaAddrBase, err = getAddress(baseSymbol, alpha); err != nil {
		return err
	}
	if betaAddrBase, err = getAddress(baseSymbol, beta); err != nil {
		return err
	}
	if alphaAddrQuote, err = getAddress(quoteSymbol, alpha); err != nil {
		return err
	}
	if betaAddrQuote, err = getAddress(quoteSymbol, beta); err != nil {
		return err
	}

	unlockWallets := func(symbol string) error {
		switch symbol {
		case btc, ltc, doge, firo, bch, dgb:
			<-harnessCtl(ctx, symbol, "./alpha", "walletpassphrase", "abc", "4294967295")
			<-harnessCtl(ctx, symbol, "./beta", "walletpassphrase", "abc", "4294967295")
		case dcr:
			<-harnessCtl(ctx, dcr, "./alpha", "walletpassphrase", "abc", "0")
			<-harnessCtl(ctx, dcr, "./beta", "walletpassphrase", "abc", "0") // creating new accounts requires wallet unlocked
			<-harnessCtl(ctx, dcr, "./beta", "unlockaccount", "default", "abc")
		case eth, zec, dextt:
			// eth unlocking for send, so no need to here. Mining
			// accounts are always unlocked. zec is unlocked already.
		default:
			return fmt.Errorf("unlockWallets: unknown symbol %q", symbol)
		}
		return nil
	}

	// Unlock wallets, since they may have been locked on a previous shutdown.
	if err = unlockWallets(baseSymbol); err != nil {
		return err
	}
	if err = unlockWallets(quoteSymbol); err != nil {
		return err
	}

	// Clean up the directory.
	os.RemoveAll(botDir)
	err = os.MkdirAll(botDir, 0700)
	if err != nil {
		return fmt.Errorf("error creating loadbot directory: %v", err)
	}
	defer os.RemoveAll(botDir)

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
			{baseID, alphaCfgBase},
			{baseID, betaCfgBase},
			{quoteID, alphaCfgQuote},
			{quoteID, betaCfgQuote},
		}

		for _, pair := range pairs {
			var walletAddr, newAddr string
			newAddr, port, err := nextAddr()
			if err != nil {
				return fmt.Errorf("unable to get a new port: %v", err)
			}
			switch pair.assetID {
			case dcrID:
				walletAddr = pair.cfg["rpclisten"]
				pair.cfg["rpclisten"] = newAddr
			case btcID, ltcID, dogeID, firoID, bchID, dgbID:
				oldPort := pair.cfg["rpcport"]
				walletAddr = "127.0.0.1:" + oldPort
				pair.cfg["rpcport"] = port
			case ethID, dexttID:
				oldPort := pair.cfg["ListenAddr"]
				walletAddr = fmt.Sprintf("127.0.0.1%s", oldPort)
				pair.cfg["ListenAddr"] = fmt.Sprintf(":%s", port)
			}
			for _, toxic := range toxics {
				name := fmt.Sprintf("%s_%d_%s", toxic.Name, pair.assetID, port)
				proxy, err := toxiClient.CreateProxy(name, newAddr, walletAddr)
				if err != nil {
					return fmt.Errorf("failed to create %s proxy: %v", toxic.Name, err)
				}
				defer proxy.Delete()
			}
		}

		for _, toxic := range toxics {
			log.Infof("Adding network condition %s", toxic.Name)
			newAddr, _, err := nextAddr()
			if err != nil {
				return fmt.Errorf("unable to get a new port: %v", err)
			}
			proxy, err := toxiClient.CreateProxy("dcrdex_"+toxic.Name, newAddr, hostAddr)
			if err != nil {
				return fmt.Errorf("failed to create %s proxy for host: %v", toxic.Name, err)
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
		runSideStacker(20, 3)
	case "compound":
		runCompound()
	case "heavy":
		runHeavy()
	case "whale":
		runWhale()
	default:
		log.Criticalf("program " + programName + " not known")
	}

	if orderCounter > 0 {
		since := time.Since(tStart)
		rate := float64(matchCounter) / (float64(since) / float64(time.Minute))
		log.Infof("LoadBot ran for %s, during which time %d orders were placed, resulting in %d separate matches, a rate of %.3g matches / minute",
			since, orderCounter, matchCounter, rate)
	}
	return nil
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
	var cfgPath string
	switch symbol {
	case eth, dextt:
		cfgPath = filepath.Join(dextestDir, harnessSymbol(symbol), node, "node", "eth.conf")
	default:
		cfgPath = filepath.Join(dextestDir, harnessSymbol(symbol), node, node+".conf")
	}
	cfg, err := config.Parse(cfgPath)
	if err != nil {
		panic(fmt.Sprintf("error parsing harness config file at %s: %v", cfgPath, err))
	}
	return cfg
}

func symmetricWalletConfig(numCoins int, midGap uint64) (
	minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty uint64) {

	minBaseQty = uint64(maxOrderLots) * uint64(numCoins) * lotSize
	minQuoteQty = calc.BaseToQuote(midGap, minBaseQty)
	// Ensure enough for registration fees.
	minBaseQty += 2e8
	minQuoteQty += 2e8
	// eth fee estimation calls for more reserves.
	// TODO: polygon and tokens
	if quoteSymbol == eth {
		add := (ethRedeemFee + ethInitFee) * uint64(maxOrderLots)
		minQuoteQty += add
	}
	if baseSymbol == eth {
		add := (ethRedeemFee + ethInitFee) * uint64(maxOrderLots)
		minBaseQty += add
	}
	maxBaseQty, maxQuoteQty = minBaseQty*2, minQuoteQty*2
	return
}
