// +build harness

package core

// The btc, dcr and dcrdex harnesses should be running before executing this
// test.
//
// The dcrdex harness rebuilds the dcrdex binary with dex.testLockTimeTaker=30s
// and dex.testLockTimeMaker=1m before running the binary, making it possible
// for this test to wait for swap locktimes to expire and ensure that refundable
// swaps are actually refunded when the swap locktimes expire.
//
// Some errors you might encounter (especially after running this test
// multiple times):
// - error placing order rpc error: 36: coin locked
//   likely that the DEX has not revoked a previously failed match that locked
//   the coin that was about to be reused, waiting a couple seconds before retrying
//   should eliminate the error. Otherwise, clear the dcrdex db and restart the
//   dcrdex harness
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
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
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

	tLockTimeTaker = 30 * time.Second
	tLockTimeMaker = 1 * time.Minute

	tLog dex.Logger
)

func readWalletCfgsAndDexCert() error {
	readText := func(path string) (string, error) {
		data, err := ioutil.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}

	user, err := user.Current()
	if err != nil {
		return err
	}

	fp := filepath.Join
	for _, client := range clients {
		dcrw, btcw := client.dcrw(), client.btcw()
		dcrw.config, err = config.Parse(fp(user.HomeDir, "dextest", "dcr", dcrw.daemon, "w-"+dcrw.daemon+".conf"))
		if err == nil {
			btcw.config, err = config.Parse(fp(user.HomeDir, "dextest", "btc", "harness-ctl", btcw.daemon+".conf"))
		}
		if err != nil {
			return err
		}
		dcrw.config["account"] = dcrw.account
		btcw.config["walletname"] = btcw.walletName
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

		// connect wallets
		for assetID, wallet := range c.wallets {
			err = c.core.CreateWallet(c.appPass, wallet.pass, &WalletForm{
				AssetID: assetID,
				Config:  wallet.config,
			})
			if err != nil {
				return err
			}
			c.log("connected %s wallet", unbip(assetID))
		}

		err = c.connectDEX(ctx)
		if err != nil {
			return err
		}
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
		DefaultLevel: slog.LevelTrace,
	})

	tLog = dex.StdOutLogger("TEST", dex.LevelTrace)

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
// Also ensures that Maker's funds are refunded after locktime expires.
func testNoTakerSwap(t *testing.T) {
	var qty, rate uint64 = 8 * 1e8, 2 * 1e4 // 8 DCR at 0.0002 BTC/DCR
	client1.isSeller, client2.isSeller = true, false
	if err := simpleTradeTest(qty, rate, order.MakerSwapCast); err != nil {
		t.Fatal(err)
	}
}

// testNoMakerRedeem runs a simple trade test and ensures that the resulting
// trades fail because of Maker not sending their redemption of Taker's swap.
// Also ensures that both Maker and Taker's funds are refunded after their
// respective swap locktime expires.
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
		if client.atFault {
			client.log("reconnecting DEX for at fault client")
			err := client.connectDEX(context.Background())
			if err != nil {
				return fmt.Errorf("client %d re-connect DEX error: %v", client.id, err)
			}
		}
		if err := client.updateBalances(); err != nil {
			return fmt.Errorf("client %d balance update error: %v", client.id, err)
		}
	}

	c1OrderID, err := client1.placeOrder(qty, rate)
	if err != nil {
		return fmt.Errorf("client1 place %s order error: %v", sellString(client1.isSeller), err)
	}
	c2OrderID, err := client2.placeOrder(qty, rate)
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

	// Check if any refunds are necessary and wait to ensure the refunds
	// are completed.
	refundsWaiter, ctx := errgroup.WithContext(context.Background())
	refundsWaiter.Go(func() error {
		return checkAndWaitForRefunds(ctx, client1, c1OrderID)
	})
	refundsWaiter.Go(func() error {
		return checkAndWaitForRefunds(ctx, client2, c2OrderID)
	})
	if err = refundsWaiter.Wait(); err != nil {
		return err
	}

	tLog.Infof("Trades ended at %s.", finalStatus)
	return nil
}

func monitorTradeForTestOrder(ctx context.Context, client *tClient, orderID string, finalStatus order.MatchStatus) error {
	errs := newErrorSet("[client %d] ", client.id)
	dc := client.dc()

	oid, err := order.IDFromHex(orderID)
	if err != nil {
		return errs.add("error parsing order id %s -> %v", orderID, err)
	}

	oidShort := token(oid.Bytes())
	tracker, _, _ := dc.findOrder(oid)

	// Wait a max of 2 epochLen durations for this order to get matched.
	maxMatchDuration := 2 * time.Duration(tracker.epochLen) * time.Millisecond
	client.log("Waiting %s for matches on order %s", maxMatchDuration, oidShort)
	matched := client.findNotification(ctx, maxMatchDuration, func(n Notification) bool {
		orderNote, isOrderNote := n.(*OrderNote)
		return isOrderNote && n.Subject() == "Matches made" && orderNote.Order.ID == orderID
	})
	if ctx.Err() != nil { // context canceled
		return nil
	}
	if !matched {
		return errs.add("order %s not matched after %s", oidShort, maxMatchDuration)
	}

	dc.tradeMtx.RLock()
	client.log("%d match(es) received for order %s", len(tracker.matches), oidShort)
	for _, match := range tracker.matches {
		client.log("%s on match %s, amount %.8f %s", match.Match.Side.String(),
			token(match.id.Bytes()), fmtAmt(match.Match.Quantity), unbip(tracker.Base()))
	}

	// Save last processed status for each match to accurately identify status
	// changes and prevent re-processing the same status for a match.
	// Set the initial processed match statuses to order.NewlyMatched to ignore
	// matches whose status have not progressed beyond the matched stage until
	// their status changes.
	lastProcessedStatus := make(map[order.MatchID]order.MatchStatus, len(tracker.matches))
	for _, match := range tracker.matches {
		lastProcessedStatus[match.id] = order.NewlyMatched
	}
	dc.tradeMtx.RUnlock()

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

	makerAtFault := finalStatus == order.NewlyMatched || finalStatus == order.TakerSwapCast
	takerAtFault := finalStatus == order.MakerSwapCast || finalStatus == order.MakerRedeemed

	// run a repeated check for match status changes to mine blocks as necessary.
	maxTradeDuration := 2 * time.Minute
	tryUntil(ctx, maxTradeDuration, func() bool {
		var completedTrades int
		dc.tradeMtx.Lock()
		defer dc.tradeMtx.Unlock()
		for _, match := range tracker.matches {
			side, status := match.Match.Side, match.Match.Status
			if status >= finalStatus {
				// We've done the needful for this match,
				// - prevent further action by blocking the match with a failErr
				// - check if this client will be suspended for inaction
				match.failErr = fmt.Errorf("take no further action")
				if (side == order.Maker && makerAtFault) || (side == order.Taker && takerAtFault) {
					client.atFault = true
				}
				completedTrades++
			}
			if status == lastProcessedStatus[match.id] || status > finalStatus {
				continue
			}
			lastProcessedStatus[match.id] = status
			client.log("NOW =====> %s", status)

			// If the new status shows that we've just sent a swap or redeem,
			// let's record appropriate balance change expectations.
			switch {
			case side == order.Maker && status == order.MakerSwapCast,
				side == order.Taker && status == order.TakerSwapCast:
				recordBalanceChanges(tracker.wallets.fromAsset.ID, true, match.Match.Quantity, match.Match.Rate)
				continue // no need to mine blocks until counter-party captures status change
			case side == order.Maker && status == order.MakerRedeemed,
				side == order.Taker && status == order.MatchComplete:
				recordBalanceChanges(tracker.wallets.toAsset.ID, false, match.Match.Quantity, match.Match.Rate)
				continue // no need to mine blocks until counter-party captures status change
			}

			// Check for and mine counter-party's swap or redeem.
			// This enables us to proceed with the required follow-up action.
			var assetToMine *dex.Asset // toAsset for counter-party's swap and fromAsset for redeem
			var swapOrRedeem string
			switch {
			case side == order.Maker && status == order.TakerSwapCast,
				side == order.Taker && status == order.MakerSwapCast:
				assetToMine, swapOrRedeem = tracker.wallets.toAsset, "swap"
			case side == order.Maker && status == order.MatchComplete,
				side == order.Taker && status == order.MakerRedeemed:
				assetToMine, swapOrRedeem = tracker.wallets.fromAsset, "redeem"
			default:
				continue
			}

			assetID, nBlocks := assetToMine.ID, uint16(assetToMine.SwapConf)
			err := mineBlocks(assetID, nBlocks)
			if err == nil {
				otherSide := order.Maker
				if side == order.Maker {
					otherSide = order.Taker
				}
				client.log("Mined %d blocks for %s's %s, match %s", nBlocks, otherSide, swapOrRedeem, token(match.id.Bytes()))
			} else {
				client.log("%s mine error %v", unbip(assetID), err)
			}
		}
		return completedTrades == len(tracker.matches)
	})
	if ctx.Err() != nil { // context canceled
		return nil
	}

	var incompleteTrades int
	dc.tradeMtx.RLock()
	for _, match := range tracker.matches {
		if match.Match.Status < finalStatus {
			incompleteTrades++
			client.log("incomplete trade: order %s, match %s, status %s, side %s", oidShort,
				token(match.ID()), match.Match.Status, match.Match.Side)
		}
	}
	dc.tradeMtx.RUnlock()
	if incompleteTrades > 0 {
		return fmt.Errorf("client %d reported %d incomplete trades for order %s after %s",
			client.id, incompleteTrades, oidShort, maxTradeDuration)
	}

	return nil
}

func checkAndWaitForRefunds(ctx context.Context, client *tClient, orderID string) error {
	// check if client has pending refunds
	client.log("checking if refunds are necessary")
	dc := client.dc()
	refundAmts := map[uint32]int64{dcr.BipID: 0, btc.BipID: 0}
	var furthestLockTime time.Time

	hasRefundableSwap := func(match *matchTracker) bool {
		sentSwap := match.MetaData.Proof.Script != nil
		noRedeems := match.Match.Status < order.MakerRedeemed
		return sentSwap && noRedeems
	}

	oid, err := order.IDFromHex(orderID)
	if err != nil {
		return fmt.Errorf("client %d: error parsing order id %s -> %v", client.id, orderID, err)
	}

	tracker, _, _ := client.dc().findOrder(oid)
	tracker.mtx.RLock()
	for _, match := range tracker.matches {
		if !hasRefundableSwap(match) {
			continue
		}

		dbMatch, _, _, auth := match.parts()
		swapAmt := dbMatch.Quantity
		if !client.isSeller {
			swapAmt = calc.BaseToQuote(dbMatch.Rate, dbMatch.Quantity)
		}
		refundAmts[tracker.wallets.fromAsset.ID] += int64(swapAmt)

		matchTime := encode.UnixTimeMilli(int64(auth.MatchStamp))
		swapLockTime := matchTime.Add(tracker.lockTimeTaker)
		if dbMatch.Side == order.Maker {
			swapLockTime = matchTime.Add(tracker.lockTimeMaker)
		}
		if swapLockTime.After(furthestLockTime) {
			furthestLockTime = swapLockTime
		}
	}
	tracker.mtx.RUnlock()

	if ctx.Err() != nil { // context canceled
		return nil
	}
	if furthestLockTime.IsZero() {
		client.log("no refunds necessary")
		return nil
	}

	client.log("found refundable swaps worth %.8f dcr and %.8f btc",
		fmtAmt(refundAmts[dcr.BipID]), fmtAmt(refundAmts[btc.BipID]))

	// wait for refunds to be executed
	now := time.Now()
	if furthestLockTime.After(now) {
		wait := furthestLockTime.Sub(now)
		client.log("waiting %s before checking wallet balances for expected refunds", wait)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}

	if refundAmts[btc.BipID] > 0 {
		// btc swaps cannot be refunded until the MedianTimePast is greater
		// than the swap locktime. The MedianTimePast is calculated by taking
		// the timestamps of the last 11 blocks and finding the median. Mining
		// 11 blocks on btc a second from now will ensure that the MedianTimePast
		// will be greater than the furthest swap locktime, thereby lifting the
		// time lock on all btc swaps.
		time.Sleep(1 * time.Second)
		if err := mineBlocks(btc.BipID, 11); err == nil {
			client.log("mined 11 btc blocks to expire swap locktimes")
		} else {
			return fmt.Errorf("client %d: error mining 11 btc blocks for swap refunds: %v",
				client.id, err)
		}
	}

	// allow up to 30 seconds for core to get around to refunding the swaps
	var notRefundedSwaps int
	refundWaitTimeout := 30 * time.Second
	refundedSwaps := tryUntil(ctx, refundWaitTimeout, func() bool {
		dc.tradeMtx.RLock()
		defer dc.tradeMtx.RUnlock()
		notRefundedSwaps = 0
		for _, match := range tracker.matches {
			if hasRefundableSwap(match) && match.MetaData.Proof.RefundCoin == nil {
				notRefundedSwaps++
			}
		}
		return notRefundedSwaps == 0
	})
	if ctx.Err() != nil { // context canceled
		return nil
	}
	if !refundedSwaps {
		return fmt.Errorf("client %d reported %d unrefunded swaps after %s",
			client.id, notRefundedSwaps, refundWaitTimeout)
	}

	// swaps refunded, mine some blocks to get the refund txs confirmed and
	// confirm that balance changes are as expected.
	for assetID, expectedBalanceDiff := range refundAmts {
		if expectedBalanceDiff > 0 {
			mineBlocks(assetID, 2)
		}
	}
	time.Sleep(5 * time.Second)

	client.expectBalanceDiffs = refundAmts
	err = client.assertBalanceChanges()
	if err == nil {
		client.log("successfully refunded swaps worth %.8f dcr and %.8f btc",
			fmtAmt(refundAmts[dcr.BipID]), fmtAmt(refundAmts[btc.BipID]))
	}
	return err
}

func tryUntil(ctx context.Context, tryDuration time.Duration, tryFn func() bool) bool {
	expire := time.NewTimer(tryDuration)
	tick := time.NewTicker(250 * time.Millisecond)
	defer func() {
		expire.Stop()
		tick.Stop()
	}()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-expire.C:
			return false
		case <-tick.C:
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
	daemon     string
	account    string // for dcr wallets
	walletName string // for btc wallets
	pass       []byte
	config     map[string]string
}

func dcrWallet(daemon string) *tWallet {
	return &tWallet{
		daemon:  daemon,
		account: "default",
		pass:    []byte("abc"),
	}
}

func btcWallet(daemon, walletName string) *tWallet {
	return &tWallet{
		daemon:     daemon,
		walletName: walletName,
		pass:       []byte("abc"),
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
	// atFault will be true if this client is guilty of inaction
	// during a test run.
	atFault bool
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
	client.core.lockTimeTaker = tLockTimeTaker
	client.core.lockTimeMaker = tLockTimeMaker
	return nil
}

func (client *tClient) connectDEX(ctx context.Context) error {
	dc := client.dc()
	if dc != nil {
		dc.connMaster.Disconnect()
		client.core.connMtx.Lock()
		delete(client.core.conns, dc.acct.host)
		client.core.connMtx.Unlock()
	}

	dexFee, err := client.core.GetFee(dexHost, dexCert)
	if err != nil {
		return err
	}

	// connect dex and pay fee
	regRes, err := client.core.Register(&RegisterForm{
		Addr:    dexHost,
		Cert:    dexCert,
		AppPass: client.appPass,
		Fee:     dexFee,
	})
	if err != nil {
		return err
	}
	client.log("connected DEX %s", dexHost)

	// mine drc block(s) to mark fee as paid
	// sometimes need to mine an extra block for fee tx to get req. confs
	err = mineBlocks(dcr.BipID, regRes.ReqConfirms+1)
	if err != nil {
		return err
	}
	client.log("mined %d dcr blocks for fee payment confirmation", regRes.ReqConfirms)

	// wait 12 seconds for fee payment, notifyfee times out after 10 seconds
	feeTimeout := 12 * time.Second
	client.log("waiting %s for fee confirmation notice", feeTimeout)
	client.notifications = client.core.NotificationFeed()
	feePaid := client.findNotification(ctx, feeTimeout, func(n Notification) bool {
		return n.Type() == "feepayment" && n.Subject() == "Account registered"
	})
	if !feePaid {
		return fmt.Errorf("fee payment not confirmed after %s", feeTimeout)
	}

	client.log("fee payment confirmed")
	return nil
}

func (client *tClient) findNotification(ctx context.Context, waitDuration time.Duration, check func(Notification) bool) bool {
	expire := time.NewTimer(waitDuration)
	defer expire.Stop()
	for {
		select {
		case n := <-client.notifications:
			if check(n) {
				return true
			}
		case <-expire.C:
			return false
		case <-ctx.Done():
			return false
		}
	}
}

func (client *tClient) placeOrder(qty, rate uint64) (string, error) {
	dc := client.dc()
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
		Sell:    client.isSeller,
		Qty:     qty,
		Rate:    rate,
		TifNow:  false,
	}

	qtyStr := fmt.Sprintf("%.8f %s", fmtAmt(qty), baseAsset.Symbol)
	rateStr := fmt.Sprintf("%.8f %s/%s", fmtAmt(rate), quoteAsset.Symbol,
		baseAsset.Symbol)

	ord, err := client.core.Trade(client.appPass, tradeForm)
	if err != nil {
		return "", err
	}

	client.log("placed order %sing %s at %s (%s)", sellString(client.isSeller), qtyStr, rateStr, ord.ID[:8])
	return ord.ID, nil
}

func (client *tClient) updateBalances() error {
	client.log("updating balances")
	client.balances = make(map[uint32]uint64, len(client.wallets))
	for assetID := range client.wallets {
		balances, err := client.core.AssetBalance(assetID)
		if err != nil {
			return err
		}
		client.balances[assetID] = balances.Available + balances.Locked
		client.log("%s available %f, locked %f", unbip(assetID),
			fmtAmt(balances.Available), fmtAmt(balances.Locked))
	}
	return nil
}

func (client *tClient) assertBalanceChanges() error {
	defer func() {
		// Clear after assertion so that the next assertion is only performed
		// if the expected balance changes are explicitly set.
		client.expectBalanceDiffs = nil
	}()
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
	lockCmd := fmt.Sprintf("./%s walletlock", dcrw.daemon)
	if err := tmuxSendKeys("dcr-harness:0", lockCmd); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	btcw := client.btcw()
	lockCmd = fmt.Sprintf("./%s -rpcwallet=%s walletlock", btcw.daemon, btcw.walletName)
	return tmuxSendKeys("btc-harness:2", lockCmd)
}

func (client *tClient) unlockWallets() error {
	client.log("unlocking wallets")
	dcrw := client.dcrw()
	unlockCmd := fmt.Sprintf("./%s walletpassphrase %q 300", dcrw.daemon, string(dcrw.pass))
	if err := tmuxSendKeys("dcr-harness:0", unlockCmd); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	btcw := client.btcw()
	unlockCmd = fmt.Sprintf("./%s -rpcwallet=%s walletpassphrase %q 300",
		btcw.daemon, btcw.walletName, string(btcw.pass))
	return tmuxSendKeys("btc-harness:2", unlockCmd)
}

func mineBlocks(assetID uint32, blocks uint16) error {
	var harnessID string
	switch assetID {
	case dcr.BipID:
		harnessID = "dcr-harness:0"
	case btc.BipID:
		harnessID = "btc-harness:2"
	default:
		return fmt.Errorf("can't mine blocks for unknown asset %d", assetID)
	}
	return tmuxSendKeys(harnessID, fmt.Sprintf("./mine-alpha %d", blocks))
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
