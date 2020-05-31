// +build harness

package core

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/wait"
	"github.com/decred/slog"
	"golang.org/x/sync/errgroup"
)

type tWallet struct {
	name    string
	account string
	pass    []byte
	config  string
	balance uint64
}

func dcrWallet(name string) *tWallet {
	return &tWallet{
		name:    name,
		account: "default",
		pass:    []byte("123"),
	}
}

func btcWallet(name, account string) *tWallet {
	return &tWallet{
		name:    name,
		account: account,
		pass:    []byte("abc"),
	}
}

type tClient struct {
	id      int
	core    *Core
	appPass []byte
	wallets map[uint32]*tWallet
}

func (client *tClient) log(format string, args ...interface{}) {
	args = append([]interface{}{client.id}, args...)
	tLog.Infof("[client %d] "+format, args...)
}

func (client *tClient) init() error {
	db, err := ioutil.TempFile("", "core.db")
	if err != nil {
		return err
	}
	client.core, err = New(&Config{
		DBPath: db.Name(),
		Net:    dex.Regtest,
	})
	return err
}

func (client *tClient) dc() *dexConnection {
	client.core.connMtx.RLock()
	defer client.core.connMtx.RUnlock()
	return client.core.conns[dexHost]
}

func (client *tClient) dcrw() *tWallet {
	return client.wallets[dcr.BipID]
}

func (client *tClient) btcw() *tWallet {
	return client.wallets[btc.BipID]
}

func mineBlocks(assetID uint32, name string, blocks uint16) error {
	var harnessID string
	switch assetID {
	case dcr.BipID:
		harnessID = "dcr-harness:4"
	case btc.BipID:
		harnessID = "btc-harness:2"
	default:
		return fmt.Errorf("can't mine blocks for unknown asset %d", assetID)
	}
	mineCmd := fmt.Sprintf("./mine-%s %d", name, blocks)
	return exec.Command("tmux", "send-keys", "-t", harnessID, mineCmd, "C-m").Run()
}

var (
	client1 = &tClient{
		id:      1,
		appPass: []byte("client1"),
		wallets: map[uint32]*tWallet{
			dcr.BipID: dcrWallet("alpha"),    // dcr alpha wallet
			btc.BipID: btcWallet("beta", ""), // btc beta wallet
		},
	}
	client2 = &tClient{
		id:      2,
		appPass: []byte("client2"),
		wallets: map[uint32]*tWallet{
			dcr.BipID: dcrWallet("beta"),           // dcr beta wallet
			btc.BipID: btcWallet("alpha", "gamma"), // btc gamma wallet
		},
	}

	dexHost = "127.0.0.1:17273"
	dexCert string

	tLog       dex.Logger
	retryQueue *wait.TickerQueue
)

func readWalletCfgsAndDexCert() error {
	readText := func(path string) (string, error) {
		cfgData, err := ioutil.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(cfgData), nil
	}

	user, err := user.Current()
	if err != nil {
		return err
	}

	readDcrConfig := func(wallet *tWallet) (err error) {
		cfgPath := filepath.Join(user.HomeDir, "dextest", "dcr", wallet.name, "w-"+wallet.name+".conf")
		wallet.config, err = readText(cfgPath)
		return err
	}
	if err = readDcrConfig(client1.dcrw()); err != nil {
		return err
	}
	if err = readDcrConfig(client2.dcrw()); err != nil {
		return err
	}

	readBtcConfig := func(wallet *tWallet) (err error) {
		cfgPath := filepath.Join(user.HomeDir, "dextest", "btc", "harness-ctl", wallet.name+".conf")
		wallet.config, err = readText(cfgPath)
		return err
	}
	if err = readBtcConfig(client1.btcw()); err != nil {
		return err
	}
	if err = readBtcConfig(client2.btcw()); err != nil {
		return err
	}

	dexCertPath := filepath.Join(user.HomeDir, "dextest", "dcrdex", "rpc.cert")
	dexCert, err = readText(dexCertPath)
	return err
}

func startClients(ctx context.Context) error {
	for _, c := range []*tClient{client1, client2} {
		err := c.init()
		c.log("core created")

		go func() {
			c.core.Run(ctx)
		}()
		time.Sleep(1 * time.Second) // wait 1s to ensure core is running before proceeding

		// init app
		err = c.core.InitializeClient(c.appPass)
		if err != nil {
			return err
		}
		c.log("core initialized")

		// connect dcr wallet, required to pay dex fees
		dcrWallet := c.dcrw()
		err = c.core.CreateWallet(c.appPass, dcrWallet.pass, &WalletForm{
			AssetID:    dcr.BipID,
			Account:    dcrWallet.account,
			ConfigText: dcrWallet.config,
		})
		if err != nil {
			return err
		}
		c.log("connected dcr wallet")

		// connect btc wallet, required to trade
		btcWallet := c.btcw()
		err = c.core.CreateWallet(c.appPass, btcWallet.pass, &WalletForm{
			AssetID:    btc.BipID,
			Account:    btcWallet.account,
			ConfigText: btcWallet.config,
		})
		if err != nil {
			return err
		}
		c.log("connected btc wallet")

		dexFee, err := c.core.GetFee(dexHost, dexCert)
		if err != nil {
			return err
		}

		// connect dex and pay fee
		err = c.core.Register(&RegisterForm{
			Addr:    dexHost,
			Cert:    dexCert,
			AppPass: c.appPass,
			Fee:     dexFee,
		})
		if err != nil {
			return err
		}
		c.log("connected DEX %s", dexHost)

		// mine drc block(s) to mark fee as paid
		regFeeConfs := c.dc().cfg.RegFeeConfirms
		err = mineBlocks(dcr.BipID, dcrWallet.name, regFeeConfs)
		if err != nil {
			return err
		}
		c.log("mined %d blocks on dcr %s for fee payment confirmation", regFeeConfs, dcrWallet.name)

		// wait for fee payment
		c.log("waiting 5 seconds for fee confirmation notice")
		notifications := c.core.NotificationFeed()
		feePaid := runWaiter(ctx, 5*time.Second, func() bool {
			select {
			case n := <-notifications:
				return n.Type() == "fee payment" && n.Subject() == "Account registered"
			default:
				return wait.TryAgain
			}
		})
		if !feePaid {
			return fmt.Errorf("fee payment not confirmed after 5 seconds")
		}
		c.log("fee payment confirmed")
	}

	return nil
}

// TestTrading runs a set of trading tests as subtests to enable performing
// setup and teardown ops. The btc, dcr and dcrdex harnesses should be running
// before executing this test.
func TestTrading(t *testing.T) {
	ctx, shutdown := context.WithCancel(context.Background())

	// defer teardown
	defer func() {
		shutdown()
		if client1.core != nil && client1.core.cfg.DBPath != "" {
			os.RemoveAll(client1.core.cfg.DBPath)
		}
		if client2.core != nil && client2.core.cfg.DBPath != "" {
			os.RemoveAll(client2.core.cfg.DBPath)
		}
	}()

	UseLoggerMaker(&dex.LoggerMaker{DefaultLevel: slog.LevelOff}) // disable core logger
	tLog = slog.NewBackend(os.Stdout).Logger("TEST")
	tLog.SetLevel(slog.LevelTrace)

	// setup
	tLog.Info("=== SETUP")
	retryQueue = wait.NewTickerQueue(1 * time.Second)
	go retryQueue.Run(ctx)
	err := readWalletCfgsAndDexCert()
	if err != nil {
		t.Fatalf("error reading wallet cfgs and dex cert, harnesses running? -> %v", err)
	}
	err = startClients(ctx)
	if err != nil {
		t.Fatalf("error starting clients: %v", err)
	}
	tLog.Info("=== SETUP COMPLETED")

	// run subtests
	tests := map[string]func(*testing.T){
		"success": testTradeSuccess,
		"failure": testTradeInterruption,
	}
	for test, testFn := range tests {
		fmt.Println() // empty line to separate test logs for easier readability
		if !t.Run(test, testFn) {
			break
		}
	}
}

func testTradeSuccess(t *testing.T) {
	c1OrderID, err := placeTestOrder(client1, true)
	if err != nil {
		t.Fatalf("client1 place order error: %v", err)
	}
	c2OrderID, err := placeTestOrder(client2, false)
	if err != nil {
		t.Fatalf("client2 place order error: %v", err)
	}

	monitorTrades, ctx := errgroup.WithContext(context.Background())
	monitorTrades.Go(func() error {
		return monitorTradeForTestOrder(ctx, client1, c1OrderID)
	})
	monitorTrades.Go(func() error {
		return monitorTradeForTestOrder(ctx, client2, c2OrderID)
	})
	err = monitorTrades.Wait()
	if err != nil {
		t.Fatal(err)
	}

	tLog.Info("Trades completed successfully")
}

func testTradeInterruption(t *testing.T) {
	t.Fatal("not implemented")
}

func placeTestOrder(c *tClient, sell bool) (string, error) {
	dc := c.dc()
	dcrBtcMkt := dc.market("dcr_btc")
	if dcrBtcMkt == nil {
		return "", fmt.Errorf("no dcr_btc market found")
	}
	baseAsset := dc.assets[dcrBtcMkt.BaseID]
	quoteAsset := dc.assets[dcrBtcMkt.QuoteID]

	tradeForm := &TradeForm{
		Host:    dexHost,
		Base:    baseAsset.ID,
		Quote:   quoteAsset.ID,
		IsLimit: true,
		Sell:    sell,
		Qty:     5 * baseAsset.LotSize,     // 5 DCR => 0.00005 BTC
		Rate:    100 * quoteAsset.RateStep, // 0.0001 BTC
		TifNow:  false,
	}

	qty := fmt.Sprintf("%.8f %s", float64(tradeForm.Qty)/conversionFactor, baseAsset.Symbol)
	rate := fmt.Sprintf("%.8f %s per %s", float64(tradeForm.Rate)/conversionFactor, quoteAsset.Symbol,
		baseAsset.Symbol)

	ord, err := c.core.Trade(c.appPass, tradeForm)
	if err != nil {
		return "", fmt.Errorf("error placing %s order %v", sellString(sell), err)
	}

	c.log("placed order %sing %s at %s (%s)", sellString(sell), qty, rate, ord.ID[:8])
	return ord.ID, nil
}

func monitorTradeForTestOrder(ctx context.Context, client *tClient, orderID string) error {
	errs := newErrorSet("[client %d] ", client.id)

	oid, err := order.IDFromHex(orderID)
	if err != nil {
		return errs.add("error parsing order id %s -> %v", orderID, err)
	}

	oidShort := token(oid.Bytes())
	client.log("waiting for matches on order %s", oidShort)

	tracker, _, _ := client.dc().findOrder(oid)
	matched := runWaiter(ctx, 15*time.Second, func() bool {
		return len(tracker.matches) > 0
	})
	if ctx.Err() != nil { // context canceled
		return nil
	}
	if !matched {
		return errs.add("order %s not matched after 15 seconds", oidShort)
	}

	client.log("%d match(es) received for order %s", len(tracker.matches), oidShort)
	for _, match := range tracker.matches {
		client.log("%s on match %s, amount %.8f", match.Match.Side.String(),
			token(match.id.Bytes()), float64(match.Match.Quantity)/conversionFactor)
	}

	// blocks will need to be mined after this client sends a swap or redeem tx.
	mineAsset := func(asset *dex.Asset) error {
		return mineBlocks(asset.ID, client.wallets[asset.ID].name, uint16(asset.SwapConf))
	}

	// set initial match statuses before running check for status changes
	matchStatuses := make(map[order.MatchID]order.MatchStatus, len(tracker.matches))
	for _, match := range tracker.matches {
		matchStatuses[match.id] = order.NewlyMatched
	}

	// run a repeated check for match status changes to mine blocks as necessary.
	runWaiter(ctx, 1*time.Minute, func() bool {
		var completedTrades int
		for _, match := range tracker.matches {
			side, status := match.Match.Side, match.Match.Status
			if status == order.MatchComplete {
				completedTrades++
			}
			if status == matchStatuses[match.id] {
				continue
			}
			matchStatuses[match.id] = status
			switch {
			// check for and mine client's swap
			case side == order.Maker && status == order.MakerSwapCast,
				side == order.Taker && status == order.TakerSwapCast:
				mineAsset(tracker.wallets.fromAsset)
				client.log("Mined %d blocks for %s swap, match %s", tracker.wallets.fromAsset.SwapConf,
					match.Match.Side, token(match.id.Bytes()))

			// check for and mine client's redeem
			case side == order.Maker && status == order.MakerRedeemed,
				side == order.Taker && status == order.MatchComplete:
				mineAsset(tracker.wallets.toAsset)
				client.log("Mined %d blocks for %s redeem, match %s", tracker.wallets.toAsset.SwapConf,
					match.Match.Side, token(match.id.Bytes()))
			}
		}
		return completedTrades == len(tracker.matches)
	})
	if ctx.Err() != nil { // context canceled
		return nil
	}

	var incompleteTrades int
	for _, match := range tracker.matches {
		if match.Match.Status < order.MakerRedeemed {
			incompleteTrades++
			client.log("incomplete trade: order %s, match %s, status %s, side %s", oidShort,
				token(match.ID()), match.Match.Status, match.Match.Side)
		}
	}
	if incompleteTrades > 0 {
		return fmt.Errorf("client %d reported %d incomplete trades for order %s after 1 minute",
			client.id, incompleteTrades, oidShort)
	}
	return nil
}

func runWaiter(ctx context.Context, tryDuration time.Duration, tryFn func() bool) bool {
	successChan := make(chan bool, 1)
	retryQueue.Wait(&wait.Waiter{
		Expiration: time.Now().Add(tryDuration),
		TryFunc: func() bool {
			select {
			case <-ctx.Done():
				successChan <- false
				return wait.DontTryAgain
			default:
				if tryFn() {
					successChan <- true
					return wait.DontTryAgain
				}
				return wait.TryAgain
			}
		},
		ExpireFunc: func() {
			successChan <- false
		},
	})
	return <-successChan
}
