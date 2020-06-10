// +build harness

package core

// The btc, dcr and dcrdex harnesses should be running before executing
// this test.
//
// Some errors you might encounter (especially after running this test
// multiple times):
// - error placing order rpc error: 22: coin locked
//   clear the dcrdex db and restart the dcrdex harness
// - error placing order not enough to cover requested funds
//   use the affected asset harness to send funds to the affected wallet
// - occasional issue with fee payment confirmation
//   restart dcr-harness and dcrdex-harness. stop dcrdex before dcr harness

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
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/dex/wait"
	"github.com/decred/slog"
	"golang.org/x/sync/errgroup"
)

var (
	client1 = &tClient{
		id:      1,
		appPass: []byte("client1"),
		wallets: map[uint32]*tWallet{
			dcr.BipID: dcrWallet("trading1"),
			btc.BipID: btcWallet("beta", "delta"),
		},
	}
	client2 = &tClient{
		id:      2,
		appPass: []byte("client2"),
		wallets: map[uint32]*tWallet{
			dcr.BipID: dcrWallet("trading2"),
			btc.BipID: btcWallet("alpha", "gamma"),
		},
	}
	clients = []*tClient{client1, client2}

	dexHost = "127.0.0.1:17273"
	dexCert string

	tLog dex.Logger
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
	for _, c := range clients {
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
		regRes, err := c.core.Register(&RegisterForm{
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
		err = mineBlocks(dcr.BipID, "alpha", regRes.ReqConfirms)
		if err != nil {
			return err
		}
		c.log("mined %d blocks on dcr %s for fee payment confirmation", regRes.ReqConfirms, dcrWallet.name)

		// wait 12 seconds for fee payment, notifyfee times out after 10 seconds
		c.log("waiting 12 seconds for fee confirmation notice")
		c.notifications = c.core.NotificationFeed()
		feePaid := tryUntil(ctx, 12*time.Second, func() bool {
			select {
			case n := <-c.notifications:
				return n.Type() == "feepayment" && n.Subject() == "Account registered"
			default:
				return wait.TryAgain
			}
		})
		if !feePaid {
			return fmt.Errorf("fee payment not confirmed after 12 seconds")
		}
		c.log("fee payment confirmed")
	}

	return nil
}

// TestTrading runs a set of trading tests as subtests to enable performing
// setup and teardown ops.
func TestTrading(t *testing.T) {
	ctx, cancelCtx := context.WithCancel(context.Background())

	// defer teardown
	defer func() {
		cancelCtx()
		if client1.core != nil && client1.core.cfg.DBPath != "" {
			os.RemoveAll(client1.core.cfg.DBPath)
		}
		if client2.core != nil && client2.core.cfg.DBPath != "" {
			os.RemoveAll(client2.core.cfg.DBPath)
		}
	}()

	UseLoggerMaker(&dex.LoggerMaker{
		Backend:      slog.NewBackend(os.Stdout),
		DefaultLevel: slog.LevelError,
	}) // core log on error only

	tLog = slog.NewBackend(os.Stdout).Logger("TEST")
	tLog.SetLevel(slog.LevelTrace)

	// setup
	tLog.Info("=== SETUP")
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
		"success":         testTradeSuccess,
		"no maker swap":   testNoMakerSwap,
		"no taker swap":   testNoTakerSwap,
		"no maker redeem": testNoMakerRedeem,
	}

	for test, testFn := range tests {
		fmt.Println() // empty line to separate test logs for better readability
		if !t.Run(test, testFn) {
			break
		}
	}
}

// testTradeSuccess runs a simple trade test and ensures that the resulting
// trades are completed successfully.
func testTradeSuccess(t *testing.T) {
	var qty, rate uint64 = 12 * 1e8, 1.5 * 1e4 // 12 DCR at 0.00015 BTC/DCR
	client1.isSeller, client2.isSeller = true, false
	if err := simpleTradeTest(qty, rate, order.MatchComplete); err != nil {
		t.Fatal(err)
	}
}

// testNoMakerSwap runs a simple trade test and ensures that the resulting
// trades fail because of the Maker not sending their init swap tx.
func testNoMakerSwap(t *testing.T) {
	var qty, rate uint64 = 10 * 1e8, 1 * 1e4 // 10 DCR at 0.0001 BTC/DCR
	client1.isSeller, client2.isSeller = false, true
	if err := simpleTradeTest(qty, rate, order.NewlyMatched); err != nil {
		t.Fatal(err)
	}
}

// testNoTakerSwap runs a simple trade test and ensures that the resulting
// trades fail because of the Taker not sending their init swap tx.
// TODO: Update the test to ensure Maker's funds are refunded after locktime
// expires.
func testNoTakerSwap(t *testing.T) {
	var qty, rate uint64 = 8 * 1e8, 2 * 1e4 // 8 DCR at 0.0002 BTC/DCR
	client1.isSeller, client2.isSeller = true, false
	if err := simpleTradeTest(qty, rate, order.MakerSwapCast); err != nil {
		t.Fatal(err)
	}
}

// testNoMakerRedeem runs a simple trade test and ensures that the resulting
// trades fail because of Maker not sending their redemption of Taker's swap.
// TODO: Update the test to ensure both Maker and Taker's funds are refunded
// after their respective swap locktime expires.
func testNoMakerRedeem(t *testing.T) {
	var qty, rate uint64 = 5 * 1e8, 2.5 * 1e4 // 5DCR at 0.00025 BTC/DCR
	client1.isSeller, client2.isSeller = true, false
	if err := simpleTradeTest(qty, rate, order.TakerSwapCast); err != nil {
		t.Fatal(err)
	}
}

// simpleTradeTest uses client1 and client2 to place similar orders but on
// either sides that get matched and monitors the resulting trades up till the
// specified final status.
// Also checks that the changes to the clients wallets balances are within
// expected range.
func simpleTradeTest(qty, rate uint64, finalStatus order.MatchStatus) error {
	if client1.isSeller && client2.isSeller {
		return fmt.Errorf("both client 1 and 2 cannot be sellers")
	}

	// Unlock wallets to place orders.
	// Also update starting balances for wallets to enable accurate
	// balance change assertion after the test completes.
	for _, client := range clients {
		if err := client.unlockWallets(); err != nil {
			return fmt.Errorf("client %d unlock wallet error: %v", client.id, err)
		}
		if err := client.updateBalances(); err != nil {
			return fmt.Errorf("client %d balance update error: %v", client.id, err)
		}
	}

	c1OrderID, err := placeTestOrder(client1, qty, rate, client1.isSeller)
	if err != nil {
		return fmt.Errorf("client1 place %s order error: %v", sellString(client1.isSeller), err)
	}
	c2OrderID, err := placeTestOrder(client2, qty, rate, client2.isSeller)
	if err != nil {
		return fmt.Errorf("client2 place %s order error: %v", sellString(client2.isSeller), err)
	}

	if finalStatus == order.NewlyMatched {
		// Lock wallets to prevent Maker from sending swap as soon as the orders are matched.
		for _, client := range clients {
			if err = client.lockWallets(); err != nil {
				return fmt.Errorf("client %d lock wallet error: %v", client.id, err)
			}
		}
	}

	monitorTrades, ctx := errgroup.WithContext(context.Background())
	monitorTrades.Go(func() error {
		return monitorTradeForTestOrder(ctx, client1, c1OrderID, finalStatus)
	})
	monitorTrades.Go(func() error {
		return monitorTradeForTestOrder(ctx, client2, c2OrderID, finalStatus)
	})
	if err = monitorTrades.Wait(); err != nil {
		return err
	}

	tLog.Infof("Trades ended at %s.", finalStatus)

	// Allow some time for balance changes to be properly reported.
	// There is usually a split-second window where a locked output
	// has been spent but the spending tx is still in mempool. This
	// will cause the txout to be included in the wallets locked
	// balance, causing a higher than actual balance report.
	time.Sleep(1 * time.Second)

	for _, client := range clients {
		if err = client.assertBalanceChanges(); err != nil {
			return fmt.Errorf("client %d balance check error: %v", client.id, err)
		}
	}

	return nil
}

func placeTestOrder(c *tClient, qty, rate uint64, sell bool) (string, error) {
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
		Qty:     qty,
		Rate:    rate,
		TifNow:  false,
	}

	qtyStr := fmt.Sprintf("%.8f %s", fmtAmt(qty), baseAsset.Symbol)
	rateStr := fmt.Sprintf("%.8f %s/%s", fmtAmt(rate), quoteAsset.Symbol,
		baseAsset.Symbol)

	ord, err := c.core.Trade(c.appPass, tradeForm)
	if err != nil {
		return "", fmt.Errorf("error placing %s order %v", sellString(sell), err)
	}

	c.log("placed order %sing %s at %s (%s)", sellString(sell), qtyStr, rateStr, ord.ID[:8])
	return ord.ID, nil
}

func monitorTradeForTestOrder(ctx context.Context, client *tClient, orderID string, finalStatus order.MatchStatus) error {
	errs := newErrorSet("[client %d] ", client.id)

	oid, err := order.IDFromHex(orderID)
	if err != nil {
		return errs.add("error parsing order id %s -> %v", orderID, err)
	}

	oidShort := token(oid.Bytes())
	client.log("waiting for matches on order %s", oidShort)

	matched := tryUntil(ctx, 15*time.Second, func() bool {
		select {
		case n := <-client.notifications:
			orderNote, isOrderNote := n.(*OrderNote)
			return isOrderNote && n.Subject() == "Matches made" && orderNote.Order.ID == orderID
		default:
			return wait.TryAgain
		}
	})
	if ctx.Err() != nil { // context canceled
		return nil
	}
	if !matched {
		return errs.add("order %s not matched after 15 seconds", oidShort)
	}

	tracker, _, _ := client.dc().findOrder(oid)
	client.log("%d match(es) received for order %s", len(tracker.matches), oidShort)
	for _, match := range tracker.matches {
		client.log("%s on match %s, amount %.8f %s", match.Match.Side.String(),
			token(match.id.Bytes()), fmtAmt(match.Match.Quantity), unbip(tracker.Base()))
	}

	// set initial match statuses before running check for status changes
	matchStatuses := make(map[order.MatchID]order.MatchStatus, len(tracker.matches))
	for _, match := range tracker.matches {
		matchStatuses[match.id] = order.NewlyMatched
	}

	// the expected balance changes for this client will be updated
	// as swaps and redeems are executed
	client.expectBalanceDiffs = map[uint32]int64{dcr.BipID: 0, btc.BipID: 0}
	recordBalanceChanges := func(assetID uint32, isSwap bool, qty, rate uint64) {
		amt := qty
		if client.isSeller != isSwap {
			// use quote amt for seller redeem and buyer swap
			amt = calc.BaseToQuote(rate, qty)
		}
		if isSwap {
			client.log("updated %s balance diff with -%f", unbip(assetID), fmtAmt(amt))
			client.expectBalanceDiffs[assetID] -= int64(amt)
		} else {
			client.log("updated %s balance diff with +%f", unbip(assetID), fmtAmt(amt))
			client.expectBalanceDiffs[assetID] += int64(amt)
		}
	}

	// run a repeated check for match status changes to mine blocks as necessary.
	tryUntil(ctx, 1*time.Minute, func() bool {
		var completedTrades int
		tracker.matchMtx.RLock()
		defer tracker.matchMtx.RUnlock()
		for _, match := range tracker.matches {
			side, status := match.Match.Side, match.Match.Status
			if status >= finalStatus {
				// prevent this client from taking any further action by blocking the match with a failErr
				match.failErr = fmt.Errorf("take no further action")
				completedTrades++
			}
			if status == matchStatuses[match.id] || status > finalStatus {
				continue
			}
			matchStatuses[match.id] = status
			client.log("NOW =====> %s", status)

			// check for and mine client's swap or redeem
			var assetToMine *dex.Asset
			var swapOrRedeem string
			switch {
			case side == order.Maker && status == order.MakerSwapCast,
				side == order.Taker && status == order.TakerSwapCast:
				assetToMine, swapOrRedeem = tracker.wallets.fromAsset, "swap"
			case side == order.Maker && status == order.MakerRedeemed,
				side == order.Taker && status == order.MatchComplete:
				assetToMine, swapOrRedeem = tracker.wallets.toAsset, "redeem"
			default:
				continue
			}

			// Wait briefly before mining swap or redeem tx. This allows the
			// monitor trade goroutine for the other client to capture and
			// handle the status change before performing the required follow
			// up action. Mining now without waiting may cause the other client
			// to perform the follow up action before the goroutine captures
			// this status change.
			time.Sleep(1 * time.Second)

			assetID, nBlocks := assetToMine.ID, uint16(assetToMine.SwapConf)
			name := client.wallets[assetID].name
			if assetID == dcr.BipID {
				name = "alpha"
			}
			err := mineBlocks(assetID, name, nBlocks)
			if err == nil {
				client.log("Mined %d blocks for %s's %s, match %s", nBlocks, side, swapOrRedeem, token(match.id.Bytes()))
			} else {
				client.log("%s %s mine error %v", unbip(assetID), name, err)
			}

			recordBalanceChanges(assetToMine.ID, swapOrRedeem == "swap", match.Match.Quantity, match.Match.Rate)
		}
		return completedTrades == len(tracker.matches)
	})
	if ctx.Err() != nil { // context canceled
		return nil
	}

	var incompleteTrades int
	for _, match := range tracker.matches {
		if match.Match.Status < finalStatus {
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

func tryUntil(ctx context.Context, tryDuration time.Duration, tryFn func() bool) bool {
	expire := time.NewTimer(tryDuration)
	defer expire.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-expire.C:
			return false
		default:
			if tryFn() {
				return true
			}
		}
	}
}

/************************************
HELPER TYPES, FUNCTIONS AND METHODS
************************************/

type tWallet struct {
	name    string
	account string
	pass    []byte
	config  string
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
	id            int
	core          *Core
	notifications <-chan Notification
	appPass       []byte
	wallets       map[uint32]*tWallet
	balances      map[uint32]uint64
	isSeller      bool
	// Update after each test run to perform post-test balance
	// change validation. Set to nil to NOT perform balance checks.
	expectBalanceDiffs map[uint32]int64
}

func (client *tClient) log(format string, args ...interface{}) {
	args = append([]interface{}{client.id}, args...)
	tLog.Infof("[client %d] "+format, args...)
}

func (client *tClient) init() error {
	db, err := ioutil.TempFile("", "dexc.db")
	if err != nil {
		return err
	}
	client.core, err = New(&Config{
		DBPath: db.Name(),
		Net:    dex.Regtest,
	})
	if err != nil {
		return err
	}
	return nil
}

func (client *tClient) updateBalances() error {
	client.log("updating balances")
	client.balances = make(map[uint32]uint64, len(client.wallets))
	for assetID := range client.wallets {
		balances, err := client.core.AssetBalances(assetID)
		if err != nil {
			return err
		}
		client.balances[assetID] = balances.ZeroConf.Available + balances.ZeroConf.Locked
	}
	return nil
}

func (client *tClient) assertBalanceChanges() error {
	prevBalances := client.balances
	err := client.updateBalances()
	if err != nil || client.expectBalanceDiffs == nil {
		return err
	}
	for assetID, expectedDiff := range client.expectBalanceDiffs {
		// actual diff wil likely be lesser than expected because of tx fees
		// TODO: account for actual fee(s) or use a more realistic fee estimate.
		minExpectedDiff, maxExpectedDiff := expectedDiff-conversionFactor, expectedDiff
		if expectedDiff == 0 {
			minExpectedDiff, maxExpectedDiff = 0, 0 // no tx fees
		}
		balanceDiff := int64(client.balances[assetID] - prevBalances[assetID])
		if balanceDiff < minExpectedDiff || balanceDiff > maxExpectedDiff {
			return fmt.Errorf("%s balance change not in expected range %.8f - %.8f, got %.8f",
				unbip(assetID), fmtAmt(minExpectedDiff), fmtAmt(maxExpectedDiff), fmtAmt(balanceDiff))
		}
		client.log("%s balance change %.8f is in expected range of %.8f - %.8f",
			unbip(assetID), fmtAmt(balanceDiff), fmtAmt(minExpectedDiff), fmtAmt(maxExpectedDiff))
	}
	return nil
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

func (client *tClient) lockWallets() error {
	client.log("locking wallets")
	dcrw := client.dcrw()
	lockCmd := fmt.Sprintf("./%s walletlock", dcrw.name)
	if err := tmuxSendKeys("dcr-harness:0", lockCmd); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	btcw := client.btcw()
	lockCmd = fmt.Sprintf("./%s -rpcwallet=%s walletlock", btcw.name, btcw.account)
	return tmuxSendKeys("btc-harness:2", lockCmd)
}

func (client *tClient) unlockWallets() error {
	client.log("unlocking wallets")
	dcrw := client.dcrw()
	unlockCmd := fmt.Sprintf("./%s walletpassphrase %q 60", dcrw.name, string(dcrw.pass))
	if err := tmuxSendKeys("dcr-harness:0", unlockCmd); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	btcw := client.btcw()
	unlockCmd = fmt.Sprintf("./%s -rpcwallet=%s walletpassphrase %q 60",
		btcw.name, btcw.account, string(btcw.pass))
	return tmuxSendKeys("btc-harness:2", unlockCmd)
}

func mineBlocks(assetID uint32, name string, blocks uint16) error {
	var harnessID string
	switch assetID {
	case dcr.BipID:
		harnessID = "dcr-harness:0"
	case btc.BipID:
		harnessID = "btc-harness:2"
	default:
		return fmt.Errorf("can't mine blocks for unknown asset %d", assetID)
	}
	mineCmd := fmt.Sprintf("./mine-%s %d", name, blocks)
	return tmuxSendKeys(harnessID, mineCmd)
}

func tmuxSendKeys(tmuxWindow, cmd string) error {
	return exec.Command("tmux", "send-keys", "-t", tmuxWindow, cmd, "C-m").Run()
}

func fmtAmt(anyAmt interface{}) float64 {
	if amt, ok := anyAmt.(uint64); ok {
		return float64(amt) / conversionFactor
	}
	if amt, ok := anyAmt.(int64); ok {
		return float64(amt) / conversionFactor
	}
	panic(fmt.Sprintf("invalid call to fmtAmt with %v", anyAmt))
}
