// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

/*
The LoadBot is a load tester for dcrdex. LoadBot works by running one or more
Trader routines, each with their own *core.Core (actually, a *Mantle) and
wallets.

Build with server locktimes in mind.
i.e. -ldflags "-X 'decred.org/dcrdex/dex.testLockTimeTaker=30s' -X 'decred.org/dcrdex/dex.testLockTimeMaker=1m'"

Supported assets are bch, btc, dash, dcr, doge, dgb, eth, firo, ltc, and zec.
*/

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
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
	_ "decred.org/dcrdex/client/asset/importall"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/config"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	dexpolygon "decred.org/dcrdex/dex/networks/polygon"
	dexsrv "decred.org/dcrdex/server/dex"
	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
)

const (
	rateEncFactor    = calc.RateEncodingFactor
	defaultBtcPerDcr = 0.000878
	alpha            = "alpha"
	beta             = "beta"
	btc              = "btc"
	dash             = "dash"
	dcr              = "dcr"
	eth              = "eth"
	polygon          = "polygon"
	firo             = "firo"
	ltc              = "ltc"
	doge             = "doge"
	dgb              = "dgb"
	bch              = "bch"
	zec              = "zec"
	zcl              = "zcl"
	usdc             = "usdc.eth"
	usdcp            = "usdc.polygon"
	maxOrderLots     = 5
	ethFeeRate       = 200 // gwei
	// missedCancelErrStr is part of an error found in dcrdex/server/market/orderrouter.go
	// that a cancel order may hit with bad timing but is not a problem.
	//
	// TODO: Consider returning a separate msgjson error from server for
	// this case.
	missedCancelErrStr = "target order not known:"

	tradingTier = 100 // ~ 50 DCR
)

var (
	dcrID, _     = dex.BipSymbolID(dcr)
	btcID, _     = dex.BipSymbolID(btc)
	ethID, _     = dex.BipSymbolID(eth)
	usdcID, _    = dex.BipSymbolID(usdc)
	polygonID, _ = dex.BipSymbolID(polygon)
	usdcpID, _   = dex.BipSymbolID(usdcp)
	ltcID, _     = dex.BipSymbolID(ltc)
	dashID, _    = dex.BipSymbolID(dash)
	dogeID, _    = dex.BipSymbolID(doge)
	dgbID, _     = dex.BipSymbolID(dgb)
	firoID, _    = dex.BipSymbolID(firo)
	bchID, _     = dex.BipSymbolID(bch)
	zecID, _     = dex.BipSymbolID(zec)
	zclID, _     = dex.BipSymbolID(zcl)
	loggerMaker  *dex.LoggerMaker
	hostAddr     = "127.0.0.1:17273"
	pass         = []byte("abc")
	log          dex.Logger
	unbip        = dex.BipIDSymbol

	usr, _     = user.Current()
	dextestDir = filepath.Join(usr.HomeDir, "dextest")
	botDir     = filepath.Join(dextestDir, fmt.Sprintf("loadbot_%d", time.Now().Unix()))

	ctx, quit = context.WithCancel(context.Background())

	supportedAssets                                                = assetMap()
	alphaAddrBase, alphaAddrQuote, market, baseSymbol, quoteSymbol string
	alphaCfgBase, betaCfgBase,
	alphaCfgQuote, betaCfgQuote map[string]string

	baseAssetCfg, quoteAssetCfg                           *dexsrv.Asset
	orderCounter, matchCounter, baseID, quoteID, regAsset uint32
	epochDuration                                         uint64
	lotSize                                               uint64
	parcelSize                                            uint64
	rateStep                                              uint64
	rateShift, rateIncrease                               int64
	bui, qui                                              dex.UnitInfo

	ethInitFee                                                 = (dexeth.InitGas(1, 0) + dexeth.RefundGas(0)) * ethFeeRate
	ethRedeemFee                                               = dexeth.RedeemGas(1, 0) * ethFeeRate
	defaultMidGap, marketBuyBuffer, whalePercent               float64
	keepMidGap, oscillate, randomOsc, ignoreErrors, liveMidGap bool
	oscInterval, oscStep, whaleFrequency                       uint64

	processesMtx sync.Mutex
	processes    []*process

	// zecSendMtx prevents sending funds too soon after mining a block and
	// the harness choosing spent outputs for Zcash.
	zecSendMtx sync.Mutex
)

func init() {
	rand.Seed(time.Now().UnixNano())
	dexeth.MaybeReadSimnetAddrs()
	dexpolygon.MaybeReadSimnetAddrs()
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
func rpcAddr(symbol string) string {
	var key string

	switch symbol {
	case dcr:
		key = "rpclisten"
	case btc, ltc, bch, zec, zcl, doge, firo, dash:
		key = "rpcport"
	case eth, usdc:
		key = "ListenAddr"
	case polygon, usdcp:
		return "" // using IPC files
	}

	if symbol == baseSymbol {
		return alphaCfgBase[key]
	}
	return alphaCfgQuote[key]
}

// returnAddress is an address for the specified node's wallet. returnAddress
// is used when a wallet accumulates more than the max allowed for some asset.
func returnAddress(symbol string) string {
	if symbol == baseSymbol {
		return alphaAddrBase
	}
	return alphaAddrQuote
}

// mine will mine a single block on the node and asset indicated.
func mine(symbol, node string) <-chan *harnessResult {
	n := 1
	switch symbol {
	case eth:
		// geth may not include some tx at first because ???. Mine more.
		n = 4
	case zec, zcl:
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
	case usdc:
		return eth
	case usdcp:
		return polygon
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
				log.Errorf("exec error (%s) %q: %v: %s", symbol, command, err, string(exitErr.Stderr))
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
	flag.IntVar(&n, "n", 0, "for compound, sniper, and sidestacker, n is the number of orders to place per epoch (default 3)")
	flag.Int64Var(&rateShift, "rateshift", 0, "for compound and sidestacker, rateShift is applied to every order and increases or decreases price by the chosen shift times the rate step, use to create a market trending in one direction (default 0)")
	flag.BoolVar(&oscillate, "oscillate", false, "for compound and sidestacker, whether the price should move up and down inside a window, use to emulate a sideways market (default false)")
	flag.BoolVar(&randomOsc, "randomosc", false, "for compound and sidestacker, oscillate more randomly")
	flag.Uint64Var(&oscInterval, "oscinterval", 300, "for compound and sidestacker, the number of epochs to take for a full oscillation cycle")
	flag.Uint64Var(&oscStep, "oscstep", 50, "for compound and sidestacker, the number of rate step to increase or decrease per epoch")
	flag.BoolVar(&ignoreErrors, "ignoreerrors", false, "log and ignore errors rather than the default behavior of stopping loadbot")
	flag.Uint64Var(&whaleFrequency, "whalefrequency", 4, "controls the frequency with which the whale \"whales\" after it is ready. To whale is to choose a rate and attempt to buy up the entire book at that price. If frequency is N, the whale will whale an average of 1 out of every N+1 epochs (default 4)")
	flag.Float64Var(&whalePercent, "whalepercent", 0.1, "The percent of the current mid gap to whale within. If 0.1 the whale will pick a target price between 0.9 and 1.1 percent of the current mid gap (default 0.1). Ignored if livemidgap is true")
	flag.BoolVar(&liveMidGap, "livemidgap", false, "set true to start with a fetched midgap. Also forces the whale to only whale towards the mid gap")
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
	var err error
	bui, err = asset.UnitInfo(baseID)
	if err != nil {
		return fmt.Errorf("cannot get base %q unit info: %v", baseSymbol, err)
	}
	qui, err = asset.UnitInfo(quoteID)
	if err != nil {
		return fmt.Errorf("cannot get quote %q unit info: %v", quoteSymbol, err)
	}

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
		parcelSize = uint64(mkt.ParcelSize)
		break
	}

	rateIncrease = int64(rateStep) * rateShift

	// Adjust to be comparable to the dcr_btc market.
	defaultMidGapConv := getXCRate(baseSymbol) / getXCRate(quoteSymbol)
	defaultMidGap = float64(calc.MessageRate(defaultMidGapConv, bui, qui)) / calc.RateEncodingFactor

	loggerMaker, err = dex.NewLoggerMaker(os.Stdout, logLevel)
	if err != nil {
		return fmt.Errorf("error creating LoggerMaker: %v", err)
	}
	log /* global */ = loggerMaker.NewLogger("LOADBOT")

	if liveMidGap {
		defaultMidGap, err = liveRate()
		if err != nil {
			return fmt.Errorf("error retrieving live rates: %v", err)
		}
	}

	if epochDuration == 0 {
		return fmt.Errorf("failed to find %q market in harness config. Available markets: %s", market, strings.Join(markets, ", "))
	}

	baseAssetCfg = mktsCfg.Assets[fmt.Sprintf("%s_simnet", strings.ToUpper(baseSymbol))]
	quoteAssetCfg = mktsCfg.Assets[fmt.Sprintf("%s_simnet", strings.ToUpper(quoteSymbol))]
	if baseAssetCfg == nil || quoteAssetCfg == nil {
		return errors.New("asset configuration missing from markets.json")
	}

	// Load the asset node configs. We'll need the wallet RPC addresses.
	// Note that the RPC address may be modified if toxiproxy is used below.
	alphaCfgBase = loadNodeConfig(baseSymbol, alpha)
	betaCfgBase = loadNodeConfig(baseSymbol, beta)
	alphaCfgQuote = loadNodeConfig(quoteSymbol, alpha)
	betaCfgQuote = loadNodeConfig(quoteSymbol, beta)

	log.Infof("Running program %s", programName)

	alphaAddress := func(symbol string) (string, error) {
		var args []string
		switch symbol {
		case btc, ltc, dgb:
			args = []string{"getnewaddress", "''", "bech32"}
		case dash, doge, bch, firo:
			args = []string{"getnewaddress"}
		case dcr:
			args = []string{"getnewaddress", "default", "ignore"}
		case eth, usdc, polygon, usdcp:
			args = []string{"attach", `--exec eth.accounts[1]`}
		case zec, zcl:
			return "tmEgW8c44RQQfft9FHXnqGp8XEcQQSRcUXD", nil // ALPHA_ADDR in the zcash harness.sh
		default:
			return "", fmt.Errorf("getAddress: unknown symbol %q", symbol)
		}
		res := <-harnessCtl(ctx, symbol, "./alpha", args...)
		if res.err != nil {
			return "", fmt.Errorf("error getting %s address: %v", symbol, res.err)
		}
		return strings.Trim(res.output, `"`), nil
	}

	if alphaAddrBase, err = alphaAddress(baseSymbol); err != nil {
		return err
	}
	if alphaAddrQuote, err = alphaAddress(quoteSymbol); err != nil {
		return err
	}

	unlockWallets := func(symbol string) error {
		switch symbol {
		case btc, ltc, dash, doge, firo, bch, dgb:
			<-harnessCtl(ctx, symbol, "./alpha", "walletpassphrase", "abc", "4294967295")
			<-harnessCtl(ctx, symbol, "./beta", "walletpassphrase", "abc", "4294967295")
		case dcr:
			<-harnessCtl(ctx, dcr, "./alpha", "walletpassphrase", "abc", "0")
			<-harnessCtl(ctx, dcr, "./beta", "walletpassphrase", "abc", "0") // creating new accounts requires wallet unlocked
			<-harnessCtl(ctx, dcr, "./beta", "unlockaccount", "default", "abc")
		case eth, zec, zcl, usdc, polygon, usdcp:
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
			case btcID, ltcID, dashID, dogeID, firoID, bchID, dgbID:
				oldPort := pair.cfg["rpcport"]
				walletAddr = "127.0.0.1:" + oldPort
				pair.cfg["rpcport"] = port
			case ethID, usdcID:
				oldPort := pair.cfg["ListenAddr"]
				walletAddr = fmt.Sprintf("127.0.0.1%s", oldPort)
				pair.cfg["ListenAddr"] = fmt.Sprintf(":%s", port)
			case polygonID, usdcpID:
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
		runSideStacker(m, n)
	case "compound":
		runCompound(m, n)
	case "whale":
		runWhale()
	case "sniper":
		runSniper(n)
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
		Base       string  `json:"base"`
		Quote      string  `json:"quote"`
		LotSize    uint64  `json:"lotSize"`
		ParcelSize uint32  `json:"parcelSize"`
		RateStep   uint64  `json:"rateStep"`
		Duration   uint64  `json:"epochDuration"`
		MBBuffer   float64 `json:"marketBuyBuffer"`
	} `json:"markets"`
	Assets map[string]*dexsrv.Asset `json:"assets"`
}

// loadNodeConfig loads the INI configuration for the specified node into a
// map[string]string.
func loadNodeConfig(symbol, node string) map[string]string {
	var cfgPath string
	switch symbol {
	case eth, usdc:
		cfgPath = filepath.Join(dextestDir, harnessSymbol(symbol), node, "node", "eth.conf")
	case polygon, usdcp:
		return map[string]string{}
	default:
		cfgPath = filepath.Join(dextestDir, harnessSymbol(symbol), node, node+".conf")
	}
	cfg, err := config.Parse(cfgPath)
	if err != nil {
		panic(fmt.Sprintf("error parsing harness config file at %s: %v", cfgPath, err))
	}
	return cfg
}

var xcRates = map[string]float64{
	"bch":           244,
	"btc":           42_000,
	"dash":          29,
	"dcr":           17,
	"dgb":           0.0083,
	"doge":          0.081,
	"eth":           2_500,
	"dextt.eth":     1,
	"usdc.eth":      1,
	"firo":          1.75,
	"ltc":           69.1,
	"polygon":       0.85,
	"dextt.polygon": 1,
	"usdc.polygon":  1,
	"weth.polygon":  2_500,
	"wbtc.polygon":  42_000,
	"zcl":           0.086,
	"zec":           22.92,
}

func getXCRate(symbol string) float64 {
	if r, found := xcRates[symbol]; found {
		return r
	}
	return 1
}

func symmetricWalletConfig() (
	minBaseQty, maxBaseQty, minQuoteQty, maxQuoteQty uint64) {

	minBaseQty = lotSize * parcelSize * tradingTier * 5 / 4
	minBaseConventional := float64(minBaseQty) / float64(bui.Conventional.ConversionFactor)
	xcB, xcQ := getXCRate(baseSymbol), getXCRate(quoteSymbol)
	minQuoteConventional := minBaseConventional * xcB / xcQ
	minQuoteQty = uint64(math.Round(minQuoteConventional * float64(qui.Conventional.ConversionFactor)))

	// Add registration fees.
	minBaseQty += tradingTier * baseAssetCfg.BondAmt
	minQuoteQty += tradingTier * quoteAssetCfg.BondAmt

	maxBaseQty, maxQuoteQty = minBaseQty*2, minQuoteQty*2
	return
}

// assetMap returns a map of asset information for supported assets.
func assetMap() map[uint32]*core.SupportedAsset {
	supported := asset.Assets()
	assets := make(map[uint32]*core.SupportedAsset, len(supported))
	for assetID, asset := range supported {
		wallet := &core.WalletState{}
		assets[assetID] = &core.SupportedAsset{
			ID:       assetID,
			Symbol:   asset.Symbol,
			Wallet:   wallet,
			Info:     asset.Info,
			Name:     asset.Info.Name,
			UnitInfo: asset.Info.UnitInfo,
		}
		for tokenID, token := range asset.Tokens {
			assets[tokenID] = &core.SupportedAsset{
				ID:       tokenID,
				Symbol:   dex.BipIDSymbol(tokenID),
				Wallet:   wallet,
				Token:    token,
				Name:     token.Name,
				UnitInfo: token.UnitInfo,
			}
		}
	}
	return assets
}

func liveRate() (float64, error) {
	b, ok := supportedAssets[baseID]
	if !ok {
		return 0.0, fmt.Errorf("%d not a supported asset", baseID)
	}
	q, ok := supportedAssets[quoteID]
	if !ok {
		return 0.0, fmt.Errorf("%d not a supported asset", quoteID)
	}
	as := map[uint32]*core.SupportedAsset{baseID: b, quoteID: q}
	liveRates := core.FetchCoinpaprikaRates(ctx, log, as)
	if len(liveRates) != 2 {
		liveRates = core.FetchMessariRates(ctx, log, as)
	}
	// If decred check dcrdata.
	if len(liveRates) != 2 && (quoteID == 42 || baseID == 42) {
		liveRates = core.FetchDcrdataRates(ctx, log, as)
	}
	if len(liveRates) != 2 {
		return 0.0, errors.New("unable to get live rates")
	}
	conversionRatio := float64(q.UnitInfo.Conventional.ConversionFactor) / float64(b.UnitInfo.Conventional.ConversionFactor)
	return liveRates[baseID] / liveRates[quoteID] * conversionRatio, nil
}
