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
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	"golang.org/x/sync/errgroup"
)

var (
	client1 = &tClient{
		id:      1,
		appPass: []byte("client1"),
		wallets: map[uint32]*tWallet{
			dcr.BipID: dcrWallet("trading1"),
			btc.BipID: btcWallet("beta", ""), // beta default ("") is encrypted, delta is not
		},
	}
	client2 = &tClient{
		id:      2,
		appPass: []byte("client2"),
		wallets: map[uint32]*tWallet{
			dcr.BipID: dcrWallet("trading2"),
			btc.BipID: btcWallet("alpha", "gamma"), // alpha default ("") is encrypted, gamma is unencrypted
		},
	}
	clients = []*tClient{client1, client2}

	dexHost = "127.0.0.1:17273"
	dexCert []byte

	// dex/testing/harness.sh => markets.json settings
	lotSize  uint64 = 10e8 // 10 DCR
	rateStep uint64 = 100  // 0.00000100 BTC/DCR

	tLockTimeTaker = 30 * time.Second
	tLockTimeMaker = 1 * time.Minute

	tLog   = dex.StdOutLogger("TEST", dex.LevelTrace)
	tmpDir string
)

func readWalletCfgsAndDexCert() error {
	readCert := func(path string) ([]byte, error) {
		data, err := ioutil.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return data, nil
	}

	user, err := user.Current()
	if err != nil {
		return err
	}

	for _, client := range clients {
		dcrw, btcw := client.wallets[dcr.BipID], client.wallets[btc.BipID]
		dcrw.config, err = config.Parse(filepath.Join(user.HomeDir, "dextest", "dcr", dcrw.daemon, dcrw.daemon+".conf"))
		if err == nil {
			btcw.config, err = config.Parse(filepath.Join(user.HomeDir, "dextest", "btc", btcw.daemon, btcw.daemon+".conf"))
		}
		if err != nil {
			return err
		}
		dcrw.config["account"] = dcrw.account
		btcw.config["walletname"] = btcw.walletName
	}

	dexCertPath := filepath.Join(user.HomeDir, "dextest", "dcrdex", "rpc.cert")
	dexCert, err = readCert(dexCertPath)
	return err
}

func startClients(ctx context.Context) error {
	for _, c := range clients {
		err := c.init(ctx)
		if err != nil {
			return err
		}
		c.log("Core created")

		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.core.Run(ctx)
		}()
		<-c.core.Ready()

		// init app
		err = c.core.InitializeClient(c.appPass)
		if err != nil {
			return err
		}
		c.log("Core initialized")

		// connect wallets
		for assetID, wallet := range c.wallets {
			err = c.core.CreateWallet(c.appPass, wallet.pass, &WalletForm{
				AssetID: assetID,
				Config:  wallet.config,
			})
			if err != nil {
				return err
			}
			c.log("Connected %s wallet", unbip(assetID))
		}

		err = c.registerDEX(ctx)
		if err != nil {
			return err
		}
	}

	return nil
}

func setup() (context.CancelFunc, error) {
	err := readWalletCfgsAndDexCert()
	if err != nil {
		return func() {}, fmt.Errorf("error reading wallet cfgs or dex cert (harnesses running?): %w", err)
	}
	ctx, cancelCtx := context.WithCancel(context.Background())
	err = startClients(ctx)
	if err != nil {
		return cancelCtx, fmt.Errorf("error starting clients: %w", err)
	}
	return cancelCtx, nil
}

func teardown(cancelCtx context.CancelFunc) {
	cancelCtx()
	for _, c := range clients {
		c.wg.Wait()
		c.log("Client %d done", c.id)
	}
	if client1.core != nil && client1.core.cfg.DBPath != "" {
		os.RemoveAll(client1.core.cfg.DBPath)
	}
	if client2.core != nil && client2.core.cfg.DBPath != "" {
		os.RemoveAll(client2.core.cfg.DBPath)
	}
}

func TestMain(m *testing.M) {
	tmpDir, _ = ioutil.TempDir("", "")
	defer os.RemoveAll(tmpDir)
	os.Exit(m.Run())
}

// TestTradeSuccess runs a simple trade test and ensures that the resulting
// trades are completed successfully.
func TestTradeSuccess(t *testing.T) {
	tLog.Info("=== SETUP")
	cancelCtx, err := setup()
	if err != nil {
		t.Fatal(err)
	}
	tLog.Info("=== SETUP COMPLETED")
	defer teardown(cancelCtx)

	var qty, rate uint64 = 2 * lotSize, 150 * rateStep // 20 DCR at 0.00015 BTC/DCR
	client1.isSeller, client2.isSeller = true, false
	simpleTradeTest(t, qty, rate, order.MatchComplete)
}

// TestNoMakerSwap runs a simple trade test and ensures that the resulting
// trades fail because of the Maker not sending their init swap tx.
func TestNoMakerSwap(t *testing.T) {
	tLog.Info("=== SETUP")
	cancelCtx, err := setup()
	if err != nil {
		t.Fatal(err)
	}
	tLog.Info("=== SETUP COMPLETED")
	defer teardown(cancelCtx)

	var qty, rate uint64 = 1 * lotSize, 100 * rateStep // 10 DCR at 0.0001 BTC/DCR
	client1.isSeller, client2.isSeller = false, true
	simpleTradeTest(t, qty, rate, order.NewlyMatched)
}

// TestNoTakerSwap runs a simple trade test and ensures that the resulting
// trades fail because of the Taker not sending their init swap tx.
// Also ensures that Maker's funds are refunded after locktime expires.
func TestNoTakerSwap(t *testing.T) {
	tLog.Info("=== SETUP")
	cancelCtx, err := setup()
	if err != nil {
		t.Fatal(err)
	}
	tLog.Info("=== SETUP COMPLETED")
	defer teardown(cancelCtx)

	var qty, rate uint64 = 3 * lotSize, 200 * rateStep // 30 DCR at 0.0002 BTC/DCR
	client1.isSeller, client2.isSeller = true, false
	simpleTradeTest(t, qty, rate, order.MakerSwapCast)
}

// TestNoMakerRedeem runs a simple trade test and ensures that the resulting
// trades fail because of Maker not redeeming Taker's swap.
// Also ensures that both Maker and Taker's funds are refunded after their
// respective swap locktime expires.
// A scenario where Maker actually redeemed Taker's swap but did not notify
// Taker is handled in TestMakerGhostingAfterTakerRedeem which ensures that
// Taker auto-finds Maker's redeem and completes the trade by redeeming Maker's
// swap.
func TestNoMakerRedeem(t *testing.T) {
	tLog.Info("=== SETUP")
	cancelCtx, err := setup()
	if err != nil {
		t.Fatal(err)
	}
	tLog.Info("=== SETUP COMPLETED")
	defer teardown(cancelCtx)

	var qty, rate uint64 = 1 * lotSize, 250 * rateStep // 10 DCR at 0.00025 BTC/DCR
	client1.isSeller, client2.isSeller = true, false
	simpleTradeTest(t, qty, rate, order.TakerSwapCast)
}

// TestMakerGhostingAfterTakerRedeem places simple orders for clients 1 and 2,
// negotiates the resulting trades smoothly till TakerSwapCast, then Maker goes
// AWOL after redeeming taker's swap without notifying Taker. This test ensures
// that Taker auto-finds Maker's redeem, extracts the secret key and redeems
// Maker's swap to complete the trade.
// A scenario where Maker actually did NOT redeem Taker's swap is handled in
// testNoMakerRedeem which ensures that both parties are able to refund their
// swaps.
// TODO: What happens if FindRedemption encounters a refund instead of a redeem?
func TestMakerGhostingAfterTakerRedeem(t *testing.T) {
	tLog.Info("=== SETUP")
	cancelCtx, err := setup()
	if err != nil {
		t.Fatal(err)
	}
	tLog.Info("=== SETUP COMPLETED")
	defer teardown(cancelCtx)

	var qty, rate uint64 = 1 * lotSize, 250 * rateStep // 10 DCR at 0.00025 BTC/DCR
	client1.isSeller, client2.isSeller = true, false

	c1OrderID, c2OrderID, err := placeTestOrders(qty, rate)
	if err != nil {
		t.Fatal(err)
	}

	// Monitor trades and stop at order.TakerSwapCast
	monitorTrades, ctx := errgroup.WithContext(context.Background())
	monitorTrades.Go(func() error {
		return monitorOrderMatchingAndTradeNeg(ctx, client1, c1OrderID, order.TakerSwapCast)
	})
	monitorTrades.Go(func() error {
		return monitorOrderMatchingAndTradeNeg(ctx, client2, c2OrderID, order.TakerSwapCast)
	})
	if err = monitorTrades.Wait(); err != nil {
		t.Fatal(err)
	}

	// Resume trades but disable Maker's ability to notify the server
	// after redeeming Taker's swap.
	resumeTrade := func(ctx context.Context, client *tClient, orderID string) error {
		tracker, err := client.findOrder(orderID)
		if err != nil {
			return err
		}
		finalStatus := order.MatchComplete
		tracker.mtx.Lock()
		for _, match := range tracker.matches {
			side, status := match.Side, match.Status
			client.log("trade %s paused at %s", token(match.MatchID[:]), status)
			if side == order.Maker {
				client.log("%s: disconnecting DEX before redeeming Taker's swap", side)
				client.dc().connMaster.Disconnect()
				finalStatus = order.MakerRedeemed // maker shouldn't get past this state
			} else {
				client.log("%s: resuming trade negotiations to audit Maker's redeem", side)
			}
			// Resume maker to redeem even though the redeem request to server
			// will fail (disconnected) after the redeem bcast.
			match.swapErr = nil
		}
		tracker.mtx.Unlock()
		// force next action since trade.tick() will not be called for disconnected dcs.
		if _, err = client.core.tick(tracker); err != nil {
			client.log("tick failure: %v", err)
		}

		return monitorTrackedTrade(ctx, client, tracker, order.TakerSwapCast, finalStatus)
	}
	resumeTrades, ctx := errgroup.WithContext(context.Background())
	resumeTrades.Go(func() error {
		return resumeTrade(ctx, client1, c1OrderID)
	})
	resumeTrades.Go(func() error {
		return resumeTrade(ctx, client2, c2OrderID)
	})
	if err = resumeTrades.Wait(); err != nil {
		t.Fatal(err)
	}

	// Allow some time for balance changes to be properly reported.
	// There is usually a split-second window where a locked output
	// has been spent but the spending tx is still in mempool. This
	// will cause the txout to be included in the wallets locked
	// balance, causing a higher than actual balance report.
	time.Sleep(1 * time.Second)

	for _, client := range clients {
		if err = client.assertBalanceChanges(); err != nil {
			t.Fatal(err)
		}
	}

	tLog.Infof("Trades completed. Maker went dark at %s, Taker continued till %s.",
		order.MakerRedeemed, order.MatchComplete)
}

// TestOrderStatusReconciliation simulates a few conditions that could cause a
// client to record a wrong status for an order, especially where the client
// considers an order as active when it no longer is. The expectation is that
// the client can infer the correct order status for such orders and update
// accordingly. The following scenarios are simulated and tested:
// Order 1:
// - Standing order, preimage not revealed, "missed" revoke_order note.
// - Expect order status to stay at Epoch status before going AWOL and to become
//   Revoked after re-connecting the DEX. Locked coins should be returned.
// Order 2:
// - Non-standing order, preimage revealed, "missed" nomatch or match request (if
//   matched).
// - Expect order status to stay at Epoch status before going AWOL and to become
//   Executed after re-connecting the DEX, even if the order was matched and the
//   matches got revoked due to client inaction.
// Order 3:
// - Standing order, partially matched, booked, revoked due to inaction on a
//   match.
// - Expect order status to be Booked before going AWOL and to become Revoked
//   after re-connecting the DEX. Locked coins should be returned.
func TestOrderStatusReconciliation(t *testing.T) {
	tLog.Info("=== SETUP")
	cancelCtx, err := setup()
	if err != nil {
		t.Fatal(err)
	}
	tLog.Info("=== SETUP COMPLETED")
	defer teardown(cancelCtx)

	for _, client := range clients {
		if err = client.updateBalances(); err != nil {
			t.Fatal(err)
		}
		client.expectBalanceDiffs = nil // not interested in balance checks for this test case
	}

	waiter, ctx := errgroup.WithContext(context.Background())

	client1.isSeller, client2.isSeller = false, true

	// Record client 2's locked balance before placing trades
	// to determine the amount locked for the placed trades.
	c2Balance, err := client2.core.AssetBalance(dcr.BipID) // client 2 is seller in dcr-btc market
	if err != nil {
		t.Fatalf("client 2 pre-trade balance error %v", err)
	}
	preTradeLockedBalance := c2Balance.Locked

	rate := 100 * rateStep // 10_000 (0.0001 BTC/DCR)

	// Place an order for client 1, qty=2*lotSize, rate=100*rateStep
	// This order should get matched to either or both of these client 2
	// sell orders:
	// - Order 2: immediate limit order, qty=2*lotSize, rate=100*rateStep,
	//            may not get matched if Order 3 below is matched first.
	// - Order 3: standing limit order, qty=4*lotSize, rate=100*rateStep,
	//            will always be partially matched (3*lotSize matched or
	//            1*lotSize matched, if Order 2 is matched first).
	waiter.Go(func() error {
		_, err := client1.placeOrder(3*lotSize, rate, false)
		if err != nil {
			return fmt.Errorf("client 1 place order error: %v", err)
		}
		return nil
	})

	// forgetClient2Order deletes the passed order id from client 2's
	// dc.trade map, ensuring that all requests and notifications for
	// the order are not processed.
	c2dc := client2.dc()
	c2ForgottenOrders := make(map[order.OrderID]*trackedTrade)
	forgetClient2Order := func(oid order.OrderID) {
		c2dc.tradeMtx.Lock()
		defer c2dc.tradeMtx.Unlock()
		tracker, found := c2dc.trades[oid]
		if !found {
			return
		}
		delete(c2dc.trades, oid)
		c2ForgottenOrders[oid] = tracker
	}

	// Expected order statuses before and after client 2 goes AWOL.
	c2OrdersBefore := make(map[order.OrderID]order.OrderStatus)
	c2OrdersAfter := make(map[order.OrderID]order.OrderStatus)
	var statusMtx sync.Mutex
	recordBeforeAfterStatuses := func(oid order.OrderID, beforeStatus, afterStatus order.OrderStatus) {
		statusMtx.Lock()
		defer statusMtx.Unlock()
		c2OrdersBefore[oid] = beforeStatus
		c2OrdersAfter[oid] = afterStatus
	}

	// Order 1:
	// - Standing order, preimage not revealed, "missed" revoke_order note.
	// - Expect order status to stay at Epoch status before going AWOL and
	//   to become Revoked after re-connecting the DEX. Locked coins should
	//   be returned.
	waiter.Go(func() error {
		// standing limit order, qty and rate doesn't matter, preimage
		// miss prevents this order from getting matched.
		orderID, err := client2.placeOrder(1*lotSize, rate, false)
		if err != nil {
			return fmt.Errorf("client 2 place order error: %v", err)
		}
		oid, err := order.IDFromHex(orderID)
		if err != nil {
			return fmt.Errorf("client 2 place order error: %v", err)
		}
		// Foil preimage reveal by "forgetting" this order.
		// Also prevents processing revoke_order notes for this order.
		forgetClient2Order(oid)
		recordBeforeAfterStatuses(oid, order.OrderStatusEpoch, order.OrderStatusRevoked)
		return nil
	})

	// Order 2:
	// - Non-standing order, preimage revealed, "missed" nomatch or match
	//   request (if matched).
	// - Expect order status to stay at Epoch status before going AWOL and
	//   to become Executed after re-connecting the DEX, even if the order
	//   was matched and the matches got revoked due to client inaction. No
	//   attempt is made to cause match revocation anyways.
	waiter.Go(func() error {
		notes := client2.startNotificationReader(ctx)
		// immediate limit order, use qty=2*lotSize, rate=300*rateStep to be
		// potentially matched by client 1's order above.
		orderID, err := client2.placeOrder(2*lotSize, rate*3, true)
		if err != nil {
			return fmt.Errorf("client 2 place order error: %v", err)
		}
		tracker, err := client2.findOrder(orderID)
		if err != nil {
			return fmt.Errorf("client 2 place order error: %v", err)
		}
		oid := tracker.ID()
		// Wait a max of 2 epochs for preimage to be sent for this order.
		twoEpochs := 2 * time.Duration(tracker.epochLen) * time.Millisecond
		client2.log("Waiting %v for preimage reveal, order %s", twoEpochs, tracker.token())
		preimageRevealed := notes.find(ctx, twoEpochs, func(n Notification) bool {
			orderNote, isOrderNote := n.(*OrderNote)
			if isOrderNote && n.Subject() == SubjectPreimageSent && orderNote.Order.ID.String() == orderID {
				forgetClient2Order(oid)
				return true
			}
			return false
		})
		if !preimageRevealed {
			return fmt.Errorf("preimage not revealed for order %s after %s", tracker.token(), twoEpochs)
		}
		recordBeforeAfterStatuses(oid, order.OrderStatusEpoch, order.OrderStatusExecuted)
		return nil
	})

	// Order 3:
	// - Standing order, partially matched, booked, revoked due to inaction on
	//   a match.
	// - Expect order status to be Booked before going AWOL and to become
	//   Revoked after re-connecting the DEX. Locked coins should be returned.
	waiter.Go(func() error {
		notes := client2.startNotificationReader(ctx)
		// standing limit order, use qty=4*lotSize, rate=100*rateStep to be
		// partially matched by client 1's order above.
		orderID, err := client2.placeOrder(4*lotSize, rate, false)
		if err != nil {
			return fmt.Errorf("client 2 place order error: %v", err)
		}
		tracker, err := client2.findOrder(orderID)
		if err != nil {
			return fmt.Errorf("client 2 place order error: %v", err)
		}
		// Wait a max of 2 epochs for preimage to be sent for this order.
		twoEpochs := 2 * time.Duration(tracker.epochLen) * time.Millisecond
		client2.log("Waiting %v for preimage reveal, order %s", twoEpochs, tracker.token())
		preimageRevealed := notes.find(ctx, twoEpochs, func(n Notification) bool {
			orderNote, isOrderNote := n.(*OrderNote)
			return isOrderNote && n.Subject() == SubjectPreimageSent && orderNote.Order.ID.String() == orderID
		})
		if !preimageRevealed {
			return fmt.Errorf("preimage not revealed for order %s after %s", tracker.token(), twoEpochs)
		}
		// Preimage sent, matches will be made soon. Lock wallets to prevent
		// client from sending swap when this order is matched. Particularly
		// important if we're matched as maker.
		client2.disableWallets()
		oid := tracker.ID()
		// Wait 1 minute for order to receive match request.
		maxMatchDuration := time.Minute
		client2.log("Waiting %v for order %s to be partially matched", maxMatchDuration, tracker.token())
		matched := notes.find(ctx, maxMatchDuration, func(n Notification) bool {
			orderNote, isOrderNote := n.(*OrderNote)
			return isOrderNote && n.Subject() == SubjectMatchesMade && orderNote.Order.ID.String() == orderID
		})
		if !matched {
			return fmt.Errorf("order %s not matched after %s", tracker.token(), maxMatchDuration)
		}
		if tracker.Trade().Remaining() == 0 {
			return fmt.Errorf("order %s fully matched instead of partially", tracker.token())
		}
		if ctx.Err() != nil {
			return nil // return here if some other goroutine errored
		}
		tracker.mtx.RLock()
		// Partially matched, let's ditch the first match to trigger order
		// revocation due to match inaction.
		var isTaker bool
		for _, match := range tracker.matches {
			match.swapErr = fmt.Errorf("ditch match")
			isTaker = match.Side == order.Taker
			break // only interested in first match
		}
		tracker.mtx.RUnlock()
		if isTaker {
			// Monitor the match till MakerSwapCast, mine a couple blocks for
			// maker's swap and ditch the match just when we're required to send
			// counter-swap.
			// Keep the order active to enable receiving audit request when Maker
			// sends swap.
			err = monitorTrackedTrade(ctx, client2, tracker, order.NewlyMatched, order.MakerSwapCast)
			if err != nil {
				return err
			}
		}
		// Match will get revoked after lastEvent+bTimeout.
		forgetClient2Order(oid) // ensure revoke_match request is "missed"
		recordBeforeAfterStatuses(oid, order.OrderStatusBooked, order.OrderStatusRevoked)
		return nil
	})

	// Wait for orders to be placed and forgotten or partly negotiated.
	if err := waiter.Wait(); err != nil {
		t.Fatal(err.Error())
	}

	tLog.Info("orders placed and monitored to desired states")

	// Confirm that the order statuses are what we expect before triggering
	// a authDEX->connect status recovery.
	c2dc.tradeMtx.RLock()
	for oid, expectStatus := range c2OrdersBefore {
		tracker, found := c2ForgottenOrders[oid]
		if !found {
			tracker, found = c2dc.trades[oid]
		}
		if !found {
			t.Fatalf("missing client 2 order %v", oid)
		}
		if tracker.metaData.Status != expectStatus {
			t.Fatalf("expected pre-recovery status %v for client 2 order %v, got %v",
				expectStatus, oid, tracker.metaData.Status)
		}
		client2.log("client 2 order %v in expected pre-recovery status %v", oid, expectStatus)
	}
	c2dc.tradeMtx.RUnlock()

	// Check trade-locked amount before disconnecting.
	c2Balance, err = client2.core.AssetBalance(dcr.BipID) // client 2 is seller in dcr-btc market
	if err != nil {
		t.Fatalf("client 2 pre-disconnect balance error %v", err)
	}
	totalLockedByTrades := c2Balance.Locked - preTradeLockedBalance
	preDisconnectLockedBalance := c2Balance.Locked   // should reduce after funds are returned
	preDisconnectAvailableBal := c2Balance.Available // should increase after funds are returned

	// Disconnect the DEX and allow some time for DEX to update order statuses.
	client2.log("Disconnecting from the DEX server")
	c2dc.connMaster.Disconnect()
	// Disconnection is asynchronous, wait for confirmation of DEX disconnection.
	disconnectTimeout := 10 * time.Second
	disconnected := client2.notes.find(context.Background(), disconnectTimeout, func(n Notification) bool {
		connNote, ok := n.(*ConnEventNote)
		return ok && connNote.Host == dexHost && !connNote.Connected
	})
	if !disconnected {
		t.Fatalf("client 2 dex not disconnected after %v", disconnectTimeout)
	}

	// Allow some time for orders to be revoked due to inaction, and
	// for requests pending on the server to expire (usually bTimeout).
	bTimeout := time.Millisecond * time.Duration(c2dc.cfg.BroadcastTimeout)
	disconnectPeriod := 2 * bTimeout
	client2.log("Waiting %v before reconnecting DEX", disconnectPeriod)
	time.Sleep(disconnectPeriod)

	client2.log("Reconnecting DEX to trigger order status reconciliation")
	// Use core.initialize to restore client 2 orders from db, and login
	// to trigger dex authentication.
	client2.core.initialize()
	_, err = client2.core.Login(client2.appPass)
	if err != nil {
		t.Fatalf("client 2 login error: %v", err)
	}

	c2dc = client2.dc()
	c2dc.tradeMtx.RLock()
	for oid, expectStatus := range c2OrdersAfter {
		tracker, found := c2dc.trades[oid]
		if !found {
			t.Fatalf("client 2 order %v not found after re-initializing core", oid)
		}
		if tracker.metaData.Status != expectStatus {
			t.Fatalf("status not updated for client 2 order %v, expected %v, got %v",
				oid, expectStatus, tracker.metaData.Status)
		}
		client2.log("Client 2 order %v in expected post-recovery status %v", oid, expectStatus)
	}
	c2dc.tradeMtx.RUnlock()

	// Wait a bit for tick cycle to trigger inactive trade retirement and funds unlocking.
	halfBTimeout := time.Millisecond * time.Duration(c2dc.cfg.BroadcastTimeout/2)
	time.Sleep(halfBTimeout)

	c2Balance, err = client2.core.AssetBalance(dcr.BipID) // client 2 is seller in dcr-btc market
	if err != nil {
		t.Fatalf("client 2 post-reconnect balance error %v", err)
	}
	if c2Balance.Available != preDisconnectAvailableBal+totalLockedByTrades {
		t.Fatalf("client 2 locked funds not returned: locked before trading %v, locked after trading %v, "+
			"locked after reconnect %v", preTradeLockedBalance, preDisconnectLockedBalance, c2Balance.Locked)
	}
	if c2Balance.Locked != preDisconnectLockedBalance-totalLockedByTrades {
		t.Fatalf("client 2 locked funds not returned: locked before trading %v, locked after trading %v, "+
			"locked after reconnect %v", preTradeLockedBalance, preDisconnectLockedBalance, c2Balance.Locked)
	}
}

// TestResendPendingRequests runs a simple trade test, simulates init/redeem
// request errors during trade negotiation and ensures that failed requests
// are retried and the trades complete successfully.
func TestResendPendingRequests(t *testing.T) {
	tLog.Info("=== SETUP")
	cancelCtx, err := setup()
	if err != nil {
		t.Fatal(err)
	}
	tLog.Info("=== SETUP COMPLETED")
	defer teardown(cancelCtx)

	var qty, rate uint64 = 1 * lotSize, 250 * rateStep // 10 DCR at 0.00025 BTC/DCR
	client1.isSeller, client2.isSeller = true, false

	c1OrderID, c2OrderID, err := placeTestOrders(qty, rate)
	if err != nil {
		t.Fatal(err)
	}

	// Monitor trades and stop at order.MakerSwapCast.
	monitorTrades, ctx := errgroup.WithContext(context.Background())
	monitorTrades.Go(func() error {
		return monitorOrderMatchingAndTradeNeg(ctx, client1, c1OrderID, order.MakerSwapCast)
	})
	monitorTrades.Go(func() error {
		return monitorOrderMatchingAndTradeNeg(ctx, client2, c2OrderID, order.MakerSwapCast)
	})
	if err = monitorTrades.Wait(); err != nil {
		t.Fatal(err)
	}

	// invalidateMatchesAndResumeNegotiations sets an invalid match ID for all
	// matches of the specified side. This ensures that susbequent attempts by
	// the client to send a match-related request to the server will fail. The
	// swapErr is also unset to resume match negotiations.
	// Returns the original match IDs for the invalidated matches.
	// Taker matches are invalidated at MakerSwapCast before taker bcasts their swap.
	// Maker matches are invalidated at TakerSwapCast before maker bcasts their redeem.
	invalidateMatchesAndResumeNegotiations := func(tracker *trackedTrade, side order.MatchSide) map[*matchTracker]order.MatchID {
		var invalidMid order.MatchID
		copy(invalidMid[:], encode.RandomBytes(32))

		tracker.mtx.Lock()
		invalidatedMatchIDs := make(map[*matchTracker]order.MatchID, len(tracker.matches))
		for _, match := range tracker.matches {
			if match.Side == side {
				invalidatedMatchIDs[match] = match.MatchID
				match.MatchID = invalidMid
			}
			match.swapErr = nil
		}
		tracker.mtx.Unlock()

		return invalidatedMatchIDs
	}

	// restoreMatchesAfterRequestErrors waits for init/redeem request errors
	// and restores match IDs to stop further errors.
	restoreMatchesAfterRequestErrors := func(client *tClient, tracker *trackedTrade, invalidatedMatchIDs map[*matchTracker]order.MatchID) error {
		// create new notification feed to catch swap-related errors from send{Init,Redeem}Async
		notes := client.core.NotificationFeed()

		var foundSwapErrorNote bool
		for !foundSwapErrorNote {
			select {
			case note := <-notes:
				foundSwapErrorNote = note.Severity() == db.ErrorLevel && note.Subject() == SubjectSwapError
			case <-time.After(time.Second):
				return fmt.Errorf("client %d: no init/redeem error note after 1 second", client.id)
			}
		}

		tracker.mtx.Lock()
		for match, mid := range invalidatedMatchIDs {
			match.MatchID = mid
		}
		tracker.mtx.Unlock()
		return nil
	}

	// Resume and monitor trades but set up both taker's init and maker's redeem
	// requests to fail.
	resumeTrade := func(ctx context.Context, client *tClient, orderID string) error {
		tracker, err := client.findOrder(orderID)
		if err != nil {
			return err
		}

		// if this is Taker, invalidate the matches to cause init request failure
		invalidatedMatchIDs := invalidateMatchesAndResumeNegotiations(tracker, order.Taker)
		client.log("resumed trade negotiations from %s", order.MakerSwapCast)

		if len(invalidatedMatchIDs) > 0 { // client is taker
			client.log("invalidated taker matches, waiting for init request error")
			if err = restoreMatchesAfterRequestErrors(client, tracker, invalidatedMatchIDs); err != nil {
				return err
			}
			client.log("taker matches restored, now monitoring trade to completion")
			return monitorTrackedTrade(ctx, client, tracker, order.MakerSwapCast, order.MatchComplete)
		}

		// client is maker, pause trade neg after auditing taker's init swap, but before sending redeem
		if err = monitorTrackedTrade(ctx, client, tracker, order.MakerSwapCast, order.TakerSwapCast); err != nil {
			return err
		}
		client.log("trade paused for maker at %s", order.TakerSwapCast)

		// invalidate maker matches to cause redeem to fail
		invalidatedMatchIDs = invalidateMatchesAndResumeNegotiations(tracker, order.Maker)
		client.log("trade resumed for maker, matches invalidated, waiting for redeem request error")
		if err = restoreMatchesAfterRequestErrors(client, tracker, invalidatedMatchIDs); err != nil {
			return err
		}
		client.log("maker matches restored, now monitoring trade to completion")
		return monitorTrackedTrade(ctx, client, tracker, order.TakerSwapCast, order.MatchComplete)
	}

	resumeTrades, ctx := errgroup.WithContext(context.Background())
	resumeTrades.Go(func() error {
		return resumeTrade(ctx, client1, c1OrderID)
	})
	resumeTrades.Go(func() error {
		return resumeTrade(ctx, client2, c2OrderID)
	})
	if err = resumeTrades.Wait(); err != nil {
		t.Fatal(err)
	}

	// Allow some time for balance changes to be properly reported.
	// There is usually a split-second window where a locked output
	// has been spent but the spending tx is still in mempool. This
	// will cause the txout to be included in the wallets locked
	// balance, causing a higher than actual balance report.
	time.Sleep(1 * time.Second)

	for _, client := range clients {
		if err = client.assertBalanceChanges(); err != nil {
			t.Fatal(err)
		}
	}

	tLog.Infof("Trades completed. Init and redeem requests failed and were resent for taker and maker respectively.")
}

// simpleTradeTest uses client1 and client2 to place similar orders but on
// either sides that get matched and monitors the resulting trades up till the
// specified final status.
// Also checks that the changes to the clients wallets balances are within
// expected range.
func simpleTradeTest(t *testing.T, qty, rate uint64, finalStatus order.MatchStatus) {
	if client1.isSeller && client2.isSeller {
		t.Fatalf("Both client 1 and 2 cannot be sellers")
	}

	c1OrderID, c2OrderID, err := placeTestOrders(qty, rate)
	if err != nil {
		t.Fatal(err)
	}

	if finalStatus == order.NewlyMatched {
		// Kill the wallets to prevent Maker from sending swap as soon as the
		// orders are matched.
		for _, client := range clients {
			client.disableWallets()
		}
	}

	// WARNING: maker/taker roles are randomly assigned to client1/client2
	// because they are in the same epoch.
	monitorTrades, ctx := errgroup.WithContext(context.Background())
	monitorTrades.Go(func() error {
		return monitorOrderMatchingAndTradeNeg(ctx, client1, c1OrderID, finalStatus)
	})
	monitorTrades.Go(func() error {
		return monitorOrderMatchingAndTradeNeg(ctx, client2, c2OrderID, finalStatus)
	})
	if err = monitorTrades.Wait(); err != nil {
		t.Fatal(err)
	}

	// Allow some time for balance changes to be properly reported.
	// There is usually a split-second window where a locked output
	// has been spent but the spending tx is still in mempool. This
	// will cause the txout to be included in the wallets locked
	// balance, causing a higher than actual balance report.
	time.Sleep(1 * time.Second)

	for _, client := range clients {
		if err = client.assertBalanceChanges(); err != nil {
			t.Fatal(err)
		}
	}

	// Check if any refunds are necessary and wait to ensure the refunds
	// are completed.
	if finalStatus != order.MatchComplete {
		refundsWaiter, ctx := errgroup.WithContext(context.Background())
		refundsWaiter.Go(func() error {
			return checkAndWaitForRefunds(ctx, client1, c1OrderID)
		})
		refundsWaiter.Go(func() error {
			return checkAndWaitForRefunds(ctx, client2, c2OrderID)
		})
		if err = refundsWaiter.Wait(); err != nil {
			t.Fatal(err)
		}
	}

	tLog.Infof("Trades ended at %s.", finalStatus)
}

func placeTestOrders(qty, rate uint64) (string, string, error) {
	for _, client := range clients {
		if err := client.updateBalances(); err != nil {
			return "", "", fmt.Errorf("client %d balance update error: %v", client.id, err)
		}
		// Reset the expected balance changes for this client, to be updated
		// later in the monitorTrackedTrade function as swaps and redeems are
		// executed.
		client.expectBalanceDiffs = map[uint32]int64{dcr.BipID: 0, btc.BipID: 0}
	}

	c1OrderID, err := client1.placeOrder(qty, rate, false)
	if err != nil {
		return "", "", fmt.Errorf("client1 place %s order error: %v", sellString(client1.isSeller), err)
	}
	c2OrderID, err := client2.placeOrder(qty, rate, false)
	if err != nil {
		return "", "", fmt.Errorf("client2 place %s order error: %v", sellString(client2.isSeller), err)
	}
	return c1OrderID, c2OrderID, nil
}

func monitorOrderMatchingAndTradeNeg(ctx context.Context, client *tClient, orderID string, finalStatus order.MatchStatus) error {
	errs := newErrorSet("[client %d] ", client.id)

	tracker, err := client.findOrder(orderID)
	if err != nil {
		return errs.addErr(err)
	}

	// Wait up to 2 times the epoch duration for this order to get matched.
	maxMatchDuration := 2 * time.Duration(tracker.epochLen) * time.Millisecond
	client.log("Waiting up to %v for matches on order %s", maxMatchDuration, tracker.token())
	matched := client.notes.find(ctx, maxMatchDuration, func(n Notification) bool {
		orderNote, isOrderNote := n.(*OrderNote)
		return isOrderNote && n.Subject() == SubjectMatchesMade && orderNote.Order.ID.String() == orderID
	})
	if ctx.Err() != nil { // context canceled
		return nil
	}
	if !matched {
		return errs.add("order %s not matched after %s", tracker.token(), maxMatchDuration)
	}

	tracker.mtx.RLock()
	client.log("%d match(es) received for order %s", len(tracker.matches), tracker.token())
	for _, match := range tracker.matches {
		client.log("%s on match %s, amount %.8f %s", match.Side.String(),
			token(match.MatchID.Bytes()), fmtAmt(match.Quantity), unbip(tracker.Base()))
	}
	tracker.mtx.RUnlock()

	return monitorTrackedTrade(ctx, client, tracker, order.NewlyMatched, finalStatus)
}

func monitorTrackedTrade(ctx context.Context, client *tClient, tracker *trackedTrade, initialStatus, finalStatus order.MatchStatus) error {
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

	// Save last processed status for each match to accurately identify status
	// changes and prevent re-processing the same status for a match.
	tracker.mtx.RLock()
	lastProcessedStatus := make(map[order.MatchID]order.MatchStatus, len(tracker.matches))
	for _, match := range tracker.matches {
		lastProcessedStatus[match.MatchID] = initialStatus
	}
	tracker.mtx.RUnlock()

	// run a repeated check for match status changes to mine blocks as necessary.
	maxTradeDuration := 2 * time.Minute
	tryUntil(ctx, maxTradeDuration, func() bool {
		var completedTrades int
		tracker.mtx.Lock()
		defer tracker.mtx.Unlock()
		for _, match := range tracker.matches {
			side, status := match.Side, match.Status
			if status >= finalStatus {
				// Prevent further action by blocking the match with a swapErr.
				match.swapErr = fmt.Errorf("take no further action")
				completedTrades++
			}
			if status == lastProcessedStatus[match.MatchID] || status > finalStatus {
				continue
			}
			lastProcessedStatus[match.MatchID] = status
			client.log("NOW =====> %s", status)

			var assetToMine *dex.Asset
			var swapOrRedeem string

			switch {
			case side == order.Maker && status == order.MakerSwapCast,
				side == order.Taker && status == order.TakerSwapCast:
				// Record expected balance changes if we've just sent a swap.
				// Do NOT mine blocks until counter-party captures status change.
				recordBalanceChanges(tracker.wallets.fromAsset.ID, true, match.Quantity, match.Rate)

			case side == order.Maker && status == order.TakerSwapCast,
				side == order.Taker && status == order.MakerSwapCast:
				// Mine block for counter-party's swap. This enables us to
				// proceed with the required follow-up action.
				// Our toAsset == counter-party's fromAsset.
				assetToMine, swapOrRedeem = tracker.wallets.toAsset, "swap"

			case status == order.MatchComplete, // maker normally jumps MakerRedeemed if 'redeem' succeeds
				side == order.Maker && status == order.MakerRedeemed:
				recordBalanceChanges(tracker.wallets.toAsset.ID, false, match.Quantity, match.Rate)
				// Mine blocks for redemption since counter-party does not wait
				// for redeem tx confirmations before performing follow-up action.
				assetToMine, swapOrRedeem = tracker.wallets.toAsset, "redeem"
			}

			if assetToMine != nil {
				assetID, nBlocks := assetToMine.ID, assetToMine.SwapConf
				err := mineBlocks(assetID, nBlocks)
				if err == nil {
					var actor order.MatchSide
					if swapOrRedeem == "redeem" {
						actor = side // this client
					} else if side == order.Maker {
						actor = order.Taker // counter-party
					} else {
						actor = order.Maker
					}
					client.log("Mined %d blocks for %s's %s, match %s", nBlocks, actor, swapOrRedeem, token(match.MatchID.Bytes()))
				} else {
					client.log("%s mine error %v", unbip(assetID), err) // return err???
				}
			}
		}
		return completedTrades == len(tracker.matches)
	})
	if ctx.Err() != nil { // context canceled
		return nil
	}

	var incompleteTrades int
	tracker.mtx.RLock()
	for _, match := range tracker.matches {
		if match.Status < finalStatus {
			incompleteTrades++
			client.log("incomplete trade: order %s, match %s, status %s, side %s", tracker.token(),
				token(match.MatchID[:]), match.Status, match.Side)
		} else {
			client.log("trade for order %s, match %s monitored successfully till %s, side %s", tracker.token(),
				token(match.MatchID[:]), match.Status, match.Side)
		}
	}
	tracker.mtx.RUnlock()
	if incompleteTrades > 0 {
		return fmt.Errorf("client %d reported %d incomplete trades for order %s after %s",
			client.id, incompleteTrades, tracker.token(), maxTradeDuration)
	}

	return nil
}

func checkAndWaitForRefunds(ctx context.Context, client *tClient, orderID string) error {
	// check if client has pending refunds
	client.log("checking if refunds are necessary")
	refundAmts := map[uint32]int64{dcr.BipID: 0, btc.BipID: 0}
	var furthestLockTime time.Time

	hasRefundableSwap := func(match *matchTracker) bool {
		sentSwap := match.MetaData.Proof.Script != nil
		noRedeems := match.Status < order.MakerRedeemed
		return sentSwap && noRedeems
	}

	tracker, err := client.findOrder(orderID)
	if err != nil {
		return err
	}

	tracker.mtx.RLock()
	for _, match := range tracker.matches {
		if !hasRefundableSwap(match) {
			continue
		}

		swapAmt := match.Quantity
		if !client.isSeller {
			swapAmt = calc.BaseToQuote(match.Rate, match.Quantity)
		}
		refundAmts[tracker.wallets.fromAsset.ID] += int64(swapAmt)

		matchTime := match.matchTime()
		swapLockTime := matchTime.Add(tracker.lockTimeTaker)
		if match.Side == order.Maker {
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
		client.log("No refunds necessary")
		return nil
	}

	client.log("Found refundable swaps worth %.8f dcr and %.8f btc",
		fmtAmt(refundAmts[dcr.BipID]), fmtAmt(refundAmts[btc.BipID]))

	// wait for refunds to be executed
	now := time.Now()
	if furthestLockTime.After(now) {
		wait := furthestLockTime.Sub(now)
		client.log("Waiting %v before checking wallet balances for expected refunds", wait)
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
			client.log("Mined 11 btc blocks to expire swap locktimes")
		} else {
			return fmt.Errorf("client %d: error mining 11 btc blocks for swap refunds: %v",
				client.id, err)
		}
	}

	// allow up to 30 seconds for core to get around to refunding the swaps
	var notRefundedSwaps int
	refundWaitTimeout := 30 * time.Second
	refundedSwaps := tryUntil(ctx, refundWaitTimeout, func() bool {
		tracker.mtx.RLock()
		defer tracker.mtx.RUnlock()
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
			if err = mineBlocks(assetID, 1); err != nil {
				return fmt.Errorf("%s mine error %v", unbip(assetID), err)
			}
		}
	}
	time.Sleep(2 * time.Second)

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
	daemon     string // indicates conf (beta.conf)
	account    string // for dcr wallets
	walletName string // for btc wallets, put into config map
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
	pass := "abc"
	if walletName == "delta" || walletName == "gamma" {
		pass = ""
	}
	return &tWallet{
		daemon:     daemon,
		walletName: walletName,
		pass:       []byte(pass),
	}
}

type tClient struct {
	id    int
	wg    sync.WaitGroup
	core  *Core
	notes *notificationReader

	appPass  []byte
	wallets  map[uint32]*tWallet
	balances map[uint32]uint64
	isSeller bool
	// Update after each test run to perform post-test balance
	// change validation. Set to nil to NOT perform balance checks.
	expectBalanceDiffs map[uint32]int64
}

func (client *tClient) log(format string, args ...interface{}) {
	args = append([]interface{}{client.id}, args...)
	tLog.Infof("[client %d] "+format, args...)
}

var clientCounter uint32

func (client *tClient) init(ctx context.Context) error {
	cNum := atomic.AddUint32(&clientCounter, 1)
	var err error
	client.core, err = New(&Config{
		DBPath: filepath.Join(tmpDir, fmt.Sprintf("dex_%d.db", cNum)),
		Net:    dex.Regtest,
		Logger: dex.StdOutLogger("CORE:"+strconv.Itoa(int(cNum)), dex.LevelTrace),
	})
	if err != nil {
		return err
	}
	// Remove backup database.
	backup := filepath.Join(tmpDir, "backup")
	os.RemoveAll(backup)

	client.core.lockTimeTaker = tLockTimeTaker
	client.core.lockTimeMaker = tLockTimeMaker
	client.notes = client.startNotificationReader(ctx)
	return nil
}

func (client *tClient) registerDEX(ctx context.Context) error {
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
	client.log("Sent registration fee to DEX %s", dexHost)

	// Mine drc block(s) to mark fee as paid.
	err = mineBlocks(dcr.BipID, uint32(regRes.ReqConfirms))
	if err != nil {
		return err
	}
	client.log("Mined %d dcr blocks for fee payment confirmation", regRes.ReqConfirms)

	// Wait up to bTimeout+12 seconds for fee payment. notify_fee times out
	// after bTimeout+10 seconds.
	feeTimeout := time.Millisecond*time.Duration(client.dc().cfg.BroadcastTimeout) + 12*time.Second
	client.log("Waiting %v for fee confirmation notice", feeTimeout)
	feePaid := client.notes.find(ctx, feeTimeout, func(n Notification) bool {
		return n.Type() == NoteTypeFeePayment && n.Subject() == SubjectAccountRegistered
	})
	if !feePaid {
		return fmt.Errorf("fee payment not confirmed after %s", feeTimeout)
	}

	client.log("Fee payment confirmed")
	return nil
}

type notificationReader struct {
	sync.Mutex
	feed  <-chan Notification
	notes []Notification
}

// startNotificationReader opens a new channel for receiving Core notifications
// and starts a goroutine to monitor the channel for new notifications to prevent
// the channel from blocking. Notifications received are added to a notes slice
// to be read by consumers subsequently.
// If multiple concurrent processes require access to Core notifications, each
// should start a notificationReader to ensure that desired notifications are
// received.
func (client *tClient) startNotificationReader(ctx context.Context) *notificationReader {
	n := &notificationReader{
		feed: client.core.NotificationFeed(),
	}

	// keep notification channel constantly drained to avoid
	// 'blocking notification channel' error logs.
	go func() {
		for {
			select {
			case note := <-n.feed:
				n.Lock()
				n.notes = append(n.notes, note)
				n.Unlock()

			case <-ctx.Done():
				return
			}
		}
	}()

	return n
}

// read returns the notifications saved by this notification reader as a slice
// of Notification objects. The notifications slice is cleared to accept new
// notifications.
func (n *notificationReader) readNotifications() []Notification {
	n.Lock()
	defer n.Unlock()
	notifications := n.notes
	n.notes = nil // mark as "read"
	return notifications
}

// find repeatedly checks the client.notifications slice for a particular
// notification until the notification is found or the specified waitDuration
// elapses. Clears the notifications slice.
func (n *notificationReader) find(ctx context.Context, waitDuration time.Duration, check func(Notification) bool) bool {
	return tryUntil(ctx, waitDuration, func() bool {
		notifications := n.readNotifications()
		for _, n := range notifications {
			if check(n) {
				return true
			}
		}
		return false
	})
}

func (client *tClient) placeOrder(qty, rate uint64, tifNow bool) (string, error) {
	dc := client.dc()
	dcrBtcMkt := dc.marketConfig("dcr_btc")
	if dcrBtcMkt == nil {
		return "", fmt.Errorf("no dcr_btc market found")
	}
	baseAsset := dc.assets[dcrBtcMkt.Base]
	quoteAsset := dc.assets[dcrBtcMkt.Quote]

	tradeForm := &TradeForm{
		Host:    dexHost,
		Base:    baseAsset.ID,
		Quote:   quoteAsset.ID,
		IsLimit: true,
		Sell:    client.isSeller,
		Qty:     qty,
		Rate:    rate,
		TifNow:  tifNow,
	}

	qtyStr := fmt.Sprintf("%.8f %s", fmtAmt(qty), baseAsset.Symbol)
	rateStr := fmt.Sprintf("%.8f %s/%s", fmtAmt(rate), quoteAsset.Symbol,
		baseAsset.Symbol)

	ord, err := client.core.Trade(client.appPass, tradeForm)
	if err != nil {
		return "", err
	}

	client.log("placed order %sing %s at %s (%s)", sellString(client.isSeller), qtyStr, rateStr, ord.ID[:8])
	return ord.ID.String(), nil
}

func (client *tClient) updateBalances() error {
	client.log("updating balances")
	client.balances = make(map[uint32]uint64, len(client.wallets))
	for assetID := range client.wallets {
		balances, err := client.core.AssetBalance(assetID)
		if err != nil {
			return err
		}
		client.balances[assetID] = balances.Available + balances.Immature + balances.Locked
		client.log("%s available %f, immature %f, locked %f", unbip(assetID),
			fmtAmt(balances.Available), fmtAmt(balances.Immature), fmtAmt(balances.Locked))
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
			return fmt.Errorf("[client %d] %s balance change not in expected range %.8f - %.8f, got %.8f",
				client.id, unbip(assetID), fmtAmt(minExpectedDiff), fmtAmt(maxExpectedDiff), fmtAmt(balanceDiff))
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

func (client *tClient) findOrder(orderID string) (*trackedTrade, error) {
	oid, err := order.IDFromHex(orderID)
	if err != nil {
		return nil, fmt.Errorf("error parsing order id %s -> %v", orderID, err)
	}
	tracker, _, _ := client.dc().findOrder(oid)
	return tracker, nil
}

// Torpedo the client's wallets by forcing (*Core).locallyUnlocked to return
// false, even for an unencrypted wallet.
func (client *tClient) disableWallets() {
	client.log("Torpedoing wallets")
	client.core.walletMtx.Lock()
	// NOTE: this is not reversible, but could be made so with the undo data:
	// walletPasses[cid] = make(map[uint32]passes, len(client.core.wallets))
	for _, wallet := range client.core.wallets {
		wallet.mtx.Lock()
		// walletPasses[cid][wid] = passes{wallet.encPW, wallet.pw}
		wallet.encPW = []byte{0}
		wallet.pw = ""
		wallet.mtx.Unlock()
	}
	client.core.walletMtx.Unlock()
}

func mineBlocks(assetID, blocks uint32) error {
	var harnessID string
	switch assetID {
	case dcr.BipID:
		harnessID = "dcr-harness:0"
	case btc.BipID:
		harnessID = "btc-harness:2"
	default:
		return fmt.Errorf("can't mine blocks for unknown asset %d", assetID)
	}
	return tmuxRun(harnessID, fmt.Sprintf("./mine-alpha %d", blocks))
}

func tmuxRun(tmuxWindow, cmd string) error {
	cmd += "; tmux wait-for -S harnessdone"
	err := exec.Command("tmux", "send-keys", "-t", tmuxWindow, cmd, "C-m").Run() // ; wait-for harnessdone
	if err != nil {
		return nil
	}
	return exec.Command("tmux", "wait-for", "harnessdone").Run()
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
