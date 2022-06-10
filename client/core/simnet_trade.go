//go:build simnet_trade_test && lgpl

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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/asset/bch"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/client/asset/doge"
	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/client/asset/ltc"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/client/db"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
	dexsrv "decred.org/dcrdex/server/dex"
	"golang.org/x/sync/errgroup"
)

var (
	dexHost = "127.0.0.1:17273"

	homeDir    = os.Getenv("HOME")
	dextestDir = filepath.Join(homeDir, "dextest")

	tLockTimeTaker = 30 * time.Second
	tLockTimeMaker = 1 * time.Minute
)

// SimClient is the configuration for the client's wallets.
type SimClient struct {
	BaseSPV   bool
	QuoteSPV  bool
	BaseNode  string
	QuoteNode string
}

type assetConfig struct {
	id               uint32
	symbol           string
	conversionFactor uint64
	valFmt           func(interface{}) string
}

var testLookup = map[string]func(s *simulationTest) error{
	"success":       testTradeSuccess,
	"nomakerswap":   testNoMakerSwap,
	"notakerswap":   testNoTakerSwap,
	"nomakerredeem": testNoMakerRedeem,
	"makerghost":    testMakerGhostingAfterTakerRedeem,
	"orderstatus":   testOrderStatusReconciliation,
	"resendpending": testResendPendingRequests,
}

func SimTests() []string {
	tests := make([]string, 0, len(testLookup))
	for name := range testLookup {
		tests = append(tests, name)
	}
	return tests
}

// SimulationConfig is the test configuration.
type SimulationConfig struct {
	BaseSymbol        string
	QuoteSymbol       string
	RegistrationAsset string
	Client1           *SimClient
	Client2           *SimClient
	Tests             []string
	Logger            dex.Logger
}

type simulationTest struct {
	ctx        context.Context
	cancel     context.CancelFunc
	log        dex.Logger
	base       *assetConfig
	quote      *assetConfig
	regAsset   uint32
	marketName string
	lotSize    uint64
	rateStep   uint64
	client1    *simulationClient
	client2    *simulationClient
	clients    []*simulationClient
}

// RunSimulationTest runs one or more simulations tests, based on the provided
// SimulationConfig.
func RunSimulationTest(cfg *SimulationConfig) error {
	if !cfg.Client1.BaseSPV && !cfg.Client2.BaseSPV && cfg.Client1.BaseNode == cfg.Client2.BaseNode {
		return fmt.Errorf("the %s RPC wallets for both clients are the same", cfg.BaseSymbol)
	}

	if !cfg.Client1.QuoteSPV && !cfg.Client2.QuoteSPV && cfg.Client1.QuoteNode == cfg.Client2.QuoteNode {
		return fmt.Errorf("the %s RPC wallets for both clients are the same", cfg.QuoteSymbol)
	}

	// No alpha wallets allowed until we smarten up the balance checks, I guess.
	if cfg.Client1.BaseNode == "alpha" || cfg.Client1.QuoteNode == "alpha" ||
		cfg.Client2.BaseNode == "alpha" || cfg.Client2.QuoteNode == "alpha" {
		return fmt.Errorf("no alpha nodes allowed")
	}

	baseID, ok := dex.BipSymbolID(cfg.BaseSymbol)
	if !ok {
		return fmt.Errorf("base asset %q not known", cfg.BaseSymbol)
	}
	baseUnitInfo, err := asset.UnitInfo(baseID)
	if err != nil {
		return fmt.Errorf("no unit info for %q", cfg.BaseSymbol)
	}
	quoteID, ok := dex.BipSymbolID(cfg.QuoteSymbol)
	if !ok {
		return fmt.Errorf("base asset %q not known", cfg.BaseSymbol)
	}
	quoteUnitInfo, err := asset.UnitInfo(quoteID)
	if err != nil {
		return fmt.Errorf("no unit info for %q", cfg.QuoteSymbol)
	}
	regAsset := baseID
	if cfg.RegistrationAsset == cfg.QuoteSymbol {
		regAsset = quoteID
	}
	valFormatter := func(valFmt func(uint64) string) func(interface{}) string {
		return func(vi interface{}) string {
			var vu uint64
			var negative bool
			switch vt := vi.(type) {
			case uint64:
				vu = vt
			case int64:
				if negative = vt < 0; negative {
					vu = uint64(-vt)
				} else {
					vu = uint64(vt)
				}
			}
			if negative {
				return "-" + valFmt(vu)
			}
			return valFmt(vu)
		}
	}

	s := &simulationTest{
		log: cfg.Logger,
		base: &assetConfig{
			id:               baseID,
			symbol:           cfg.BaseSymbol,
			conversionFactor: baseUnitInfo.Conventional.ConversionFactor,
			valFmt:           valFormatter(baseUnitInfo.ConventionalString),
		},
		quote: &assetConfig{
			id:               quoteID,
			symbol:           cfg.QuoteSymbol,
			conversionFactor: quoteUnitInfo.Conventional.ConversionFactor,
			valFmt:           valFormatter(quoteUnitInfo.ConventionalString),
		},
		regAsset:   regAsset,
		marketName: marketName(baseID, quoteID),
	}

	if err := s.setup(cfg.Client1, cfg.Client2); err != nil {
		return fmt.Errorf("setup error: %w", err)
	}

	for _, testName := range cfg.Tests {
		s.log.Infof("Running %q", testName)
		f, ok := testLookup[testName]
		if !ok {
			return fmt.Errorf("no test named %q", testName)
		}
		if err := f(s); err != nil {
			return fmt.Errorf("test %q failed: %w", testName, err)
		}
	}

	s.cancel()
	for _, c := range s.clients {
		c.wg.Wait()
		c.log.Infof("Client %s done", c.name)
	}

	return nil
}

func (s *simulationTest) startClients() error {
	for _, c := range s.clients {
		err := c.init(s.ctx)
		if err != nil {
			return err
		}
		c.log.Infof("Core created")

		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.core.Run(s.ctx)
		}()
		<-c.core.Ready()

		// init app
		err = c.core.InitializeClient(c.appPass, nil)
		if err != nil {
			return err
		}
		c.log.Infof("Core initialized")

		// connect wallets
		for assetID, wallet := range c.wallets {
			os.RemoveAll(c.core.assetDataDirectory(assetID))

			c.log.Infof("Creating %s %s-type wallet. config = %+v", dex.BipIDSymbol(assetID), wallet.walletType, wallet.config)

			err = c.core.CreateWallet(c.appPass, wallet.pass, &WalletForm{
				Type:    wallet.walletType,
				AssetID: assetID,
				Config:  wallet.config,
			})
			if err != nil {
				return err
			}
			c.log.Infof("Connected %s wallet (fund = %v)", unbip(assetID), wallet.fund)

			if wallet.fund {
				hctrl := newHarnessCtrl(assetID)
				// eth needs the headers to be new in order to
				// count itself synced, so mining a few blocks here.
				hctrl.mineBlocks(s.ctx, 1)
				c.log.Infof("Waiting for %s wallet to sync", unbip(assetID))
				for !c.core.WalletState(assetID).Synced {
					time.Sleep(time.Second)
				}
				c.log.Infof("%s wallet synced and ready to use", unbip(assetID))

				// Fund new wallet.
				c.log.Infof("Funding wallet")
				address := c.core.WalletState(assetID).Address
				amts := []int{10, 18, 5, 7, 1, 15, 3, 25}
				hctrl.fund(s.ctx, address, amts)
				hctrl.mineBlocks(s.ctx, 2)
				// Tip change after block filtering scan takes the wallet time.
				time.Sleep(2 * time.Second * sleepFactor)
			}
		}

		err = s.registerDEX(c)
		if err != nil {
			return err
		}
	}

	dc, _, err := s.client1.core.dex(dexHost)
	if err != nil {
		return err
	}

	mktCfg := dc.marketConfig(s.marketName)
	s.lotSize = mktCfg.LotSize
	s.rateStep = mktCfg.RateStep

	return nil
}

func (s *simulationTest) setup(cl1, cl2 *SimClient) (err error) {
	s.client1, err = s.newClient("1", cl1)
	if err != nil {
		return err
	}
	s.client2, err = s.newClient("2", cl2)
	if err != nil {
		return err
	}
	s.clients = []*simulationClient{s.client1, s.client2}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	err = s.startClients()
	if err != nil {
		return fmt.Errorf("error starting clients: %w", err)
	}
	return nil
}

var sleepFactor time.Duration = 1

// TestTradeSuccess runs a simple trade test and ensures that the resulting
// trades are completed successfully.
func testTradeSuccess(s *simulationTest) error {
	var qty, rate uint64 = 2 * s.lotSize, 150 * s.rateStep
	s.client1.isSeller, s.client2.isSeller = true, false
	return s.simpleTradeTest(qty, rate, order.MatchComplete)
}

// TestNoMakerSwap runs a simple trade test and ensures that the resulting
// trades fail because of the Maker not sending their init swap tx.
func testNoMakerSwap(s *simulationTest) error {
	var qty, rate uint64 = 1 * s.lotSize, 100 * s.rateStep
	s.client1.isSeller, s.client2.isSeller = false, true
	return s.simpleTradeTest(qty, rate, order.NewlyMatched)
}

// TestNoTakerSwap runs a simple trade test and ensures that the resulting
// trades fail because of the Taker not sending their init swap tx.
// Also ensures that Maker's funds are refunded after locktime expires.
func testNoTakerSwap(s *simulationTest) error {
	var qty, rate uint64 = 3 * s.lotSize, 200 * s.rateStep
	s.client1.isSeller, s.client2.isSeller = true, false
	return s.simpleTradeTest(qty, rate, order.MakerSwapCast)
}

// TestNoMakerRedeem runs a simple trade test and ensures that the resulting
// trades fail because of Maker not redeeming Taker's swap.
// Also ensures that both Maker and Taker's funds are refunded after their
// respective swap locktime expires.
// A scenario where Maker actually redeemed Taker's swap but did not notify
// Taker is handled in TestMakerGhostingAfterTakerRedeem which ensures that
// Taker auto-finds Maker's redeem and completes the trade by redeeming Maker's
// swap.
func testNoMakerRedeem(s *simulationTest) error {
	var qty, rate uint64 = 1 * s.lotSize, 250 * s.rateStep
	s.client1.isSeller, s.client2.isSeller = true, false
	return s.simpleTradeTest(qty, rate, order.TakerSwapCast)
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
func testMakerGhostingAfterTakerRedeem(s *simulationTest) error {
	var qty, rate uint64 = 1 * s.lotSize, 250 * s.rateStep
	s.client1.isSeller, s.client2.isSeller = true, false

	c1OrderID, c2OrderID, err := s.placeTestOrders(qty, rate)
	if err != nil {
		return err
	}

	// Monitor trades and stop at order.TakerSwapCast
	monitorTrades, ctx := errgroup.WithContext(context.Background())
	monitorTrades.Go(func() error {
		return s.monitorOrderMatchingAndTradeNeg(ctx, s.client1, c1OrderID, order.TakerSwapCast)
	})
	monitorTrades.Go(func() error {
		return s.monitorOrderMatchingAndTradeNeg(ctx, s.client2, c2OrderID, order.TakerSwapCast)
	})
	if err = monitorTrades.Wait(); err != nil {
		return err
	}

	// Resume trades but disable Maker's ability to notify the server
	// after redeeming Taker's swap.
	resumeTrade := func(ctx context.Context, client *simulationClient, orderID string) error {
		tracker, err := client.findOrder(orderID)
		if err != nil {
			return err
		}
		finalStatus := order.MatchComplete
		var disconnectClient bool
		tracker.mtx.Lock()
		for _, match := range tracker.matches {
			side, status := match.Side, match.Status
			client.log.Infof("trade %s paused at %s", token(match.MatchID[:]), status)
			if side == order.Maker {
				// Disconnecting the client will lock the
				// tracker mutex, and so cannot be done with it already locked.
				disconnectClient = true
			} else {
				client.log.Infof("%s: resuming trade negotiations to audit Maker's redeem", side)
				client.noRedeemWait = true
			}
			// Resume maker to redeem even though the redeem request to server
			// will fail (disconnected) after the redeem bcast.
			match.swapErr = nil
		}
		tracker.mtx.Unlock()
		if disconnectClient {
			client.log.Infof("Maker: disconnecting DEX before redeeming Taker's swap")
			client.dc().connMaster.Disconnect()
			finalStatus = order.MakerRedeemed // maker shouldn't get past this state
		}
		// force next action since trade.tick() will not be called for disconnected dcs.
		if _, err = client.core.tick(tracker); err != nil {
			client.log.Infof("tick failure: %v", err)
		}

		// Propagation to miners can take some time after the send RPC
		// completes, especially with SPV wallets, so wait a bit before mining
		// blocks in monitorTrackedTrade.
		time.Sleep(sleepFactor * time.Second)

		return s.monitorTrackedTrade(client, tracker, finalStatus)
	}
	resumeTrades, ctx := errgroup.WithContext(context.Background())
	resumeTrades.Go(func() error {
		return resumeTrade(ctx, s.client1, c1OrderID)
	})
	resumeTrades.Go(func() error {
		return resumeTrade(ctx, s.client2, c2OrderID)
	})
	if err = resumeTrades.Wait(); err != nil {
		return err
	}

	// Allow some time for balance changes to be properly reported.
	// There is usually a split-second window where a locked output
	// has been spent but the spending tx is still in mempool. This
	// will cause the txout to be included in the wallets locked
	// balance, causing a higher than actual balance report.
	time.Sleep(4 * sleepFactor * time.Second)

	for _, client := range s.clients {
		if err = s.assertBalanceChanges(client); err != nil {
			return err
		}
	}

	s.log.Infof("Trades completed. Maker went dark at %s, Taker continued till %s.",
		order.MakerRedeemed, order.MatchComplete)
	return nil
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
func testOrderStatusReconciliation(s *simulationTest) error {
	for _, client := range s.clients {
		if err := s.updateBalances(client); err != nil {
			return err
		}
		client.expectBalanceDiffs = nil // not interested in balance checks for this test case
	}

	waiter, ctx := errgroup.WithContext(context.Background())

	s.client1.isSeller, s.client2.isSeller = false, true

	// Record client 2's locked balance before placing trades
	// to determine the amount locked for the placed trades.
	c2Balance, err := s.client2.core.AssetBalance(s.base.id) // client 2 is seller
	if err != nil {
		return fmt.Errorf("client 2 pre-trade balance error %w", err)
	}
	preTradeLockedBalance := c2Balance.Locked

	rate := 100 * s.rateStep

	// Place an order for client 1, qty=2*lotSize, rate=100*rateStep
	// This order should get matched to either or both of these client 2
	// sell orders:
	// - Order 2: immediate limit order, qty=2*lotSize, rate=100*rateStep,
	//            may not get matched if Order 3 below is matched first.
	// - Order 3: standing limit order, qty=4*lotSize, rate=100*rateStep,
	//            will always be partially matched (3*lotSize matched or
	//            1*lotSize matched, if Order 2 is matched first).
	waiter.Go(func() error {
		_, err := s.placeOrder(s.client1, 3*s.rateStep, rate, false)
		if err != nil {
			return fmt.Errorf("client 1 place order error: %v", err)
		}
		return nil
	})

	// forgetClient2Order deletes the passed order id from client 2's
	// dc.trade map, ensuring that all requests and notifications for
	// the order are not processed.
	c2dc := s.client2.dc()
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
		orderID, err := s.placeOrder(s.client2, 1*s.lotSize, rate, false)
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
		notes := s.client2.startNotificationReader(ctx)
		// immediate limit order, use qty=2*lotSize, rate=300*rateStep to be
		// potentially matched by client 1's order above.
		orderID, err := s.placeOrder(s.client2, 2*s.lotSize, rate*3, true)
		if err != nil {
			return fmt.Errorf("client 2 place order error: %v", err)
		}
		tracker, err := s.client2.findOrder(orderID)
		if err != nil {
			return fmt.Errorf("client 2 place order error: %v", err)
		}
		oid := tracker.ID()
		// Wait a max of 2 epochs for preimage to be sent for this order.
		twoEpochs := 2 * time.Duration(tracker.epochLen) * time.Millisecond
		s.client2.log.Infof("Waiting %v for preimage reveal, order %s", twoEpochs, tracker.token())
		preimageRevealed := notes.find(ctx, twoEpochs, func(n Notification) bool {
			orderNote, isOrderNote := n.(*OrderNote)
			if isOrderNote && n.Topic() == TopicPreimageSent && orderNote.Order.ID.String() == orderID {
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
		notes := s.client2.startNotificationReader(ctx)
		// standing limit order, use qty=4*lotSize, rate=100*rateStep to be
		// partially matched by client 1's order above.
		orderID, err := s.placeOrder(s.client2, 4*s.lotSize, rate, false)
		if err != nil {
			return fmt.Errorf("client 2 place order error: %v", err)
		}
		tracker, err := s.client2.findOrder(orderID)
		if err != nil {
			return fmt.Errorf("client 2 place order error: %v", err)
		}
		// Wait a max of 2 epochs for preimage to be sent for this order.
		twoEpochs := 2 * time.Duration(tracker.epochLen) * time.Millisecond
		s.client2.log.Infof("Waiting %v for preimage reveal, order %s", twoEpochs, tracker.token())
		preimageRevealed := notes.find(ctx, twoEpochs, func(n Notification) bool {
			orderNote, isOrderNote := n.(*OrderNote)
			return isOrderNote && n.Topic() == TopicPreimageSent && orderNote.Order.ID.String() == orderID
		})
		if !preimageRevealed {
			return fmt.Errorf("preimage not revealed for order %s after %s", tracker.token(), twoEpochs)
		}
		// Preimage sent, matches will be made soon. Lock wallets to prevent
		// client from sending swap when this order is matched. Particularly
		// important if we're matched as maker.
		s.client2.disableWallets()
		oid := tracker.ID()
		// Wait 1 minute for order to receive match request.
		maxMatchDuration := time.Minute
		s.client2.log.Infof("Waiting %v for order %s to be partially matched", maxMatchDuration, tracker.token())
		matched := notes.find(ctx, maxMatchDuration, func(n Notification) bool {
			orderNote, isOrderNote := n.(*OrderNote)
			return isOrderNote && n.Topic() == TopicMatchesMade && orderNote.Order.ID.String() == orderID
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
			err = s.monitorTrackedTrade(s.client2, tracker, order.MakerSwapCast)
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
		return err
	}

	s.log.Info("orders placed and monitored to desired states")

	// Confirm that the order statuses are what we expect before triggering
	// a authDEX->connect status recovery.
	c2dc.tradeMtx.RLock()
	for oid, expectStatus := range c2OrdersBefore {
		tracker, found := c2ForgottenOrders[oid]
		if !found {
			tracker, found = c2dc.trades[oid]
		}
		if !found {
			return fmt.Errorf("missing client 2 order %v", oid)
		}
		if tracker.metaData.Status != expectStatus {
			return fmt.Errorf("expected pre-recovery status %v for client 2 order %v, got %v",
				expectStatus, oid, tracker.metaData.Status)
		}
		s.client2.log.Infof("client 2 order %v in expected pre-recovery status %v", oid, expectStatus)
	}
	c2dc.tradeMtx.RUnlock()

	// Check trade-locked amount before disconnecting.
	c2Balance, err = s.client2.core.AssetBalance(s.base.id) // client 2 is seller
	if err != nil {
		return fmt.Errorf("client 2 pre-disconnect balance error %w", err)
	}
	totalLockedByTrades := c2Balance.Locked - preTradeLockedBalance
	preDisconnectLockedBalance := c2Balance.Locked   // should reduce after funds are returned
	preDisconnectAvailableBal := c2Balance.Available // should increase after funds are returned

	// Disconnect the DEX and allow some time for DEX to update order statuses.
	s.client2.log.Infof("Disconnecting from the DEX server")
	c2dc.connMaster.Disconnect()
	// Disconnection is asynchronous, wait for confirmation of DEX disconnection.
	disconnectTimeout := 10 * sleepFactor * time.Second
	disconnected := s.client2.notes.find(context.Background(), disconnectTimeout, func(n Notification) bool {
		connNote, ok := n.(*ConnEventNote)
		return ok && connNote.Host == dexHost && connNote.ConnectionStatus != comms.Connected
	})
	if !disconnected {
		return fmt.Errorf("client 2 dex not disconnected after %v", disconnectTimeout)
	}

	// Disconnect the wallets, they'll be reconnected when Login is called below.
	// Login->connectWallets will error for btc spv wallets if the wallet is not
	// first disconnected.
	s.client2.disconnectWallets()

	// Allow some time for orders to be revoked due to inaction, and
	// for requests pending on the server to expire (usually bTimeout).
	bTimeout := time.Millisecond * time.Duration(c2dc.cfg.BroadcastTimeout)
	disconnectPeriod := 2 * bTimeout
	s.client2.log.Infof("Waiting %v before reconnecting DEX", disconnectPeriod)
	time.Sleep(disconnectPeriod)

	s.client2.log.Infof("Reconnecting DEX to trigger order status reconciliation")
	// Use core.initialize to restore client 2 orders from db, and login
	// to trigger dex authentication.
	// TODO: cannot do this anymore with built-in wallets
	s.client2.core.initialize()
	_, err = s.client2.core.Login(s.client2.appPass)
	if err != nil {
		return fmt.Errorf("client 2 login error: %w", err)
	}

	c2dc = s.client2.dc()
	c2dc.tradeMtx.RLock()
	for oid, expectStatus := range c2OrdersAfter {
		tracker, found := c2dc.trades[oid]
		if !found {
			return fmt.Errorf("client 2 order %v not found after re-initializing core", oid)
		}
		if tracker.metaData.Status != expectStatus {
			return fmt.Errorf("status not updated for client 2 order %v, expected %v, got %v",
				oid, expectStatus, tracker.metaData.Status)
		}
		s.client2.log.Infof("Client 2 order %v in expected post-recovery status %v", oid, expectStatus)
	}
	c2dc.tradeMtx.RUnlock()

	// Wait a bit for tick cycle to trigger inactive trade retirement and funds unlocking.
	halfBTimeout := time.Millisecond * time.Duration(c2dc.cfg.BroadcastTimeout/2)
	time.Sleep(halfBTimeout)

	c2Balance, err = s.client2.core.AssetBalance(s.base.id) // client 2 is seller
	if err != nil {
		return fmt.Errorf("client 2 post-reconnect balance error %w", err)
	}
	if c2Balance.Available != preDisconnectAvailableBal+totalLockedByTrades {
		return fmt.Errorf("client 2 locked funds not returned: locked before trading %v, locked after trading %v, "+
			"locked after reconnect %v", preTradeLockedBalance, preDisconnectLockedBalance, c2Balance.Locked)
	}
	if c2Balance.Locked != preDisconnectLockedBalance-totalLockedByTrades {
		return fmt.Errorf("client 2 locked funds not returned: locked before trading %v, locked after trading %v, "+
			"locked after reconnect %v", preTradeLockedBalance, preDisconnectLockedBalance, c2Balance.Locked)
	}
	return nil
}

// TestResendPendingRequests runs a simple trade test, simulates init/redeem
// request errors during trade negotiation and ensures that failed requests
// are retried and the trades complete successfully.
func testResendPendingRequests(s *simulationTest) error {
	var qty, rate uint64 = 1 * s.lotSize, 250 * s.rateStep
	s.client1.isSeller, s.client2.isSeller = true, false

	c1OrderID, c2OrderID, err := s.placeTestOrders(qty, rate)
	if err != nil {
		return err
	}

	// Monitor trades and stop at order.MakerSwapCast.
	monitorTrades, ctx := errgroup.WithContext(context.Background())
	monitorTrades.Go(func() error {
		return s.monitorOrderMatchingAndTradeNeg(ctx, s.client1, c1OrderID, order.MakerSwapCast)
	})
	monitorTrades.Go(func() error {
		return s.monitorOrderMatchingAndTradeNeg(ctx, s.client2, c2OrderID, order.MakerSwapCast)
	})
	if err = monitorTrades.Wait(); err != nil {
		return err
	}

	// TODO: Rethink how we trigger resendPendingRequests, because causing an
	// error with a request, esp. by hacking it's match ID to be incorrect, is
	// very indirect and can cause a data race by modifying a field that is
	// supposed to be immutable. Can we just call resendPendingRequests?

	// invalidateMatchesAndResumeNegotiations sets an invalid match ID for all
	// matches of the specified side. This ensures that subsequent attempts by
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
	restoreMatchesAfterRequestErrors := func(client *simulationClient, tracker *trackedTrade, invalidatedMatchIDs map[*matchTracker]order.MatchID) error {
		// create new notification feed to catch swap-related errors from send{Init,Redeem}Async
		notes := client.core.NotificationFeed()

		wait := 5 * sleepFactor * time.Second
		timer := time.NewTimer(wait)
		defer timer.Stop()
		var foundSwapErrorNote bool
		for !foundSwapErrorNote {
			select {
			case note := <-notes:
				foundSwapErrorNote = note.Severity() == db.ErrorLevel && (note.Topic() == TopicSwapSendError ||
					note.Topic() == TopicInitError || note.Topic() == TopicReportRedeemError)
			case <-timer.C:
				return fmt.Errorf("client %s: no init/redeem error note after %v", client.name, wait)
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
	resumeTrade := func(ctx context.Context, client *simulationClient, orderID string) error {
		tracker, err := client.findOrder(orderID)
		if err != nil {
			return err
		}

		// if this is Taker, invalidate the matches to cause init request failure
		invalidatedMatchIDs := invalidateMatchesAndResumeNegotiations(tracker, order.Taker)
		client.log.Infof("resumed trade negotiations from %s", order.MakerSwapCast)

		if len(invalidatedMatchIDs) > 0 { // client is taker
			client.log.Infof("invalidated taker matches, waiting for init request error")
			if err = restoreMatchesAfterRequestErrors(client, tracker, invalidatedMatchIDs); err != nil {
				return err
			}
			client.log.Infof("taker matches restored, now monitoring trade to completion")
			return s.monitorTrackedTrade(client, tracker, order.MatchComplete)
		}

		// client is maker, pause trade neg after auditing taker's init swap, but before sending redeem
		if err = s.monitorTrackedTrade(client, tracker, order.TakerSwapCast); err != nil {
			return err
		}
		client.log.Infof("trade paused for maker at %s", order.TakerSwapCast)

		// invalidate maker matches to cause redeem to fail
		invalidatedMatchIDs = invalidateMatchesAndResumeNegotiations(tracker, order.Maker)
		client.log.Infof("trade resumed for maker, matches invalidated, waiting for redeem request error")
		if err = restoreMatchesAfterRequestErrors(client, tracker, invalidatedMatchIDs); err != nil {
			return err
		}
		client.log.Infof("maker matches restored, now monitoring trade to completion")
		return s.monitorTrackedTrade(client, tracker, order.MatchComplete)
	}

	resumeTrades, ctx := errgroup.WithContext(context.Background())
	resumeTrades.Go(func() error {
		return resumeTrade(ctx, s.client1, c1OrderID)
	})
	resumeTrades.Go(func() error {
		return resumeTrade(ctx, s.client2, c2OrderID)
	})
	if err = resumeTrades.Wait(); err != nil {
		return err
	}

	// Allow some time for balance changes to be properly reported.
	// There is usually a split-second window where a locked output
	// has been spent but the spending tx is still in mempool. This
	// will cause the txout to be included in the wallet's locked
	// balance, causing a higher than actual balance report.
	time.Sleep(4 * sleepFactor * time.Second)

	for _, client := range s.clients {
		if err = s.assertBalanceChanges(client); err != nil {
			return err
		}
	}

	s.log.Infof("Trades completed. Init and redeem requests failed and were resent for taker and maker respectively.")
	return nil
}

// simpleTradeTest uses client1 and client2 to place similar orders but on
// either sides that get matched and monitors the resulting trades up till the
// specified final status.
// Also checks that the changes to the clients wallets balances are within
// expected range.
func (s *simulationTest) simpleTradeTest(qty, rate uint64, finalStatus order.MatchStatus) error {
	if s.client1.isSeller && s.client2.isSeller {
		return fmt.Errorf("Both client 1 and 2 cannot be sellers")
	}

	c1OrderID, c2OrderID, err := s.placeTestOrders(qty, rate)
	if err != nil {
		return err
	}

	if finalStatus == order.NewlyMatched {
		// Kill the wallets to prevent Maker from sending swap as soon as the
		// orders are matched.
		for _, client := range s.clients {
			client.disableWallets()
		}
	}

	// WARNING: maker/taker roles are randomly assigned to client1/client2
	// because they are in the same epoch.
	monitorTrades, ctx := errgroup.WithContext(context.Background())
	monitorTrades.Go(func() error {
		return s.monitorOrderMatchingAndTradeNeg(ctx, s.client1, c1OrderID, finalStatus)
	})
	monitorTrades.Go(func() error {
		return s.monitorOrderMatchingAndTradeNeg(ctx, s.client2, c2OrderID, finalStatus)
	})
	if err = monitorTrades.Wait(); err != nil {
		return err
	}

	// Allow some time for balance changes to be properly reported.
	// There is usually a split-second window where a locked output
	// has been spent but the spending tx is still in mempool. This
	// will cause the txout to be included in the wallets locked
	// balance, causing a higher than actual balance report.
	time.Sleep(4 * sleepFactor * time.Second)

	for _, client := range s.clients {
		if err = s.assertBalanceChanges(client); err != nil {
			return err
		}
	}

	// Check if any refunds are necessary and wait to ensure the refunds
	// are completed.
	if finalStatus != order.MatchComplete {
		refundsWaiter, ctx := errgroup.WithContext(context.Background())
		refundsWaiter.Go(func() error {
			return s.checkAndWaitForRefunds(ctx, s.client1, c1OrderID)
		})
		refundsWaiter.Go(func() error {
			return s.checkAndWaitForRefunds(ctx, s.client2, c2OrderID)
		})
		if err = refundsWaiter.Wait(); err != nil {
			return err
		}
	}

	s.log.Infof("Trades ended at %s.", finalStatus)
	return nil
}

func (s *simulationTest) placeTestOrders(qty, rate uint64) (string, string, error) {
	for _, client := range s.clients {
		if err := s.updateBalances(client); err != nil {
			return "", "", fmt.Errorf("client %s balance update error: %v", client.name, err)
		}
		// Reset the expected balance changes for this client, to be updated
		// later in the monitorTrackedTrade function as swaps and redeems are
		// executed.
		client.expectBalanceDiffs = map[uint32]int64{s.base.id: 0, s.quote.id: 0}
	}

	c1OrderID, err := s.placeOrder(s.client1, qty, rate, false)
	if err != nil {
		return "", "", fmt.Errorf("client1 place %s order error: %v", sellString(s.client1.isSeller), err)
	}
	c2OrderID, err := s.placeOrder(s.client2, qty, rate, false)
	if err != nil {
		return "", "", fmt.Errorf("client2 place %s order error: %v", sellString(s.client2.isSeller), err)
	}
	return c1OrderID, c2OrderID, nil
}

func (s *simulationTest) monitorOrderMatchingAndTradeNeg(ctx context.Context, client *simulationClient, orderID string, finalStatus order.MatchStatus) error {
	errs := newErrorSet("[client %s] ", client.name)

	tracker, err := client.findOrder(orderID)
	if err != nil {
		return errs.addErr(err)
	}

	// Wait up to 2 times the epoch duration for this order to get matched.
	maxMatchDuration := 2 * time.Duration(tracker.epochLen) * time.Millisecond
	client.log.Infof("Waiting up to %v for matches on order %s", maxMatchDuration, tracker.token())
	matched := client.notes.find(ctx, maxMatchDuration, func(n Notification) bool {
		orderNote, isOrderNote := n.(*OrderNote)
		return isOrderNote && n.Topic() == TopicMatchesMade && orderNote.Order.ID.String() == orderID
	})
	if ctx.Err() != nil { // context canceled
		return nil
	}
	if !matched {
		return errs.add("order %s not matched after %s", tracker.token(), maxMatchDuration)
	}

	tracker.mtx.RLock()
	client.log.Infof("%d match(es) received for order %s", len(tracker.matches), tracker.token())
	for _, match := range tracker.matches {
		client.log.Infof("%s on match %s, amount %f %s", match.Side.String(), token(match.MatchID.Bytes()),
			s.base.valFmt(match.Quantity), s.base.symbol)
	}
	tracker.mtx.RUnlock()

	return s.monitorTrackedTrade(client, tracker, finalStatus)
}

func (s *simulationTest) monitorTrackedTrade(client *simulationClient, tracker *trackedTrade, finalStatus order.MatchStatus) error {
	recordBalanceChanges := func(isSwap bool, qty, rate uint64) {
		amt := int64(qty)
		a := s.base
		if client.isSeller != isSwap {
			// use quote amt for seller redeem and buyer swap
			amt = int64(calc.BaseToQuote(rate, qty))
			a = s.quote
		}
		if isSwap {
			amt *= -1
		}
		client.log.Infof("updated %s balance diff with %s", a.symbol, a.valFmt(amt))
		client.expectBalanceDiffs[a.id] += amt
	}

	// run a repeated check for match status changes to mine blocks as necessary.
	maxTradeDuration := 2 * time.Minute
	tryUntil(s.ctx, maxTradeDuration, func() bool {
		var completedTrades int
		tracker.mtx.Lock()
		defer tracker.mtx.Unlock()
		for _, match := range tracker.matches {
			side, status := match.Side, match.Status
			if status >= finalStatus {
				// With async redeem request, wait for redeem sig before
				// declaring the match complete. Sometimes it is almost
				// instantaneos and other times the server's node takes a while.
				// to see the txns (TODO: diagnose this).
				// Tests that don't expect a redeem can set noRedeemWait to true.
				if finalStatus == order.MatchComplete &&
					len(match.MetaData.Proof.Auth.RedeemSig) == 0 &&
					!client.noRedeemWait {
					client.log.Infof("Completed match waiting for async redeem response...")
					continue // check again later
				}
				// Prevent further action by blocking the match with a swapErr.
				match.swapErr = fmt.Errorf("take no further action")
				completedTrades++
			}

			client.psMTX.Lock()
			if status == client.processedStatus[match.MatchID] || status > finalStatus {
				client.psMTX.Unlock()
				continue
			}
			lastStatus := client.processedStatus[match.MatchID]
			client.processedStatus[match.MatchID] = status
			client.psMTX.Unlock()
			client.log.Infof("NOW =====> %s", status)

			var assetToMine *dex.Asset
			var swapOrRedeem string

			switch {
			case side == order.Maker && status == order.MakerSwapCast,
				side == order.Taker && status == order.TakerSwapCast:
				// Record expected balance changes if we've just sent a swap.
				// Do NOT mine blocks until counter-party captures status change.
				recordBalanceChanges(true, match.Quantity, match.Rate)

			case side == order.Maker && status == order.TakerSwapCast,
				side == order.Taker && status == order.MakerSwapCast:
				// Mine block for counter-party's swap. This enables us to
				// proceed with the required follow-up action.
				// Our toAsset == counter-party's fromAsset.
				assetToMine, swapOrRedeem = tracker.wallets.toAsset, "swap"

			case side == order.Maker && status == order.MakerRedeemed,
				status == order.MatchComplete && (side == order.Taker || lastStatus != order.MakerRedeemed): // allow MatchComplete for Maker if lastStatus != order.MakerRedeemed
				recordBalanceChanges(false, match.Quantity, match.Rate)
				// Mine blocks for redemption since counter-party does not wait
				// for redeem tx confirmations before performing follow-up action.
				assetToMine, swapOrRedeem = tracker.wallets.toAsset, "redeem"
			}

			if assetToMine != nil {
				assetID, nBlocks := assetToMine.ID, assetToMine.SwapConf
				time.Sleep(2 * sleepFactor * time.Second)
				err := newHarnessCtrl(assetID).mineBlocks(s.ctx, nBlocks)
				if err == nil {
					var actor order.MatchSide
					if swapOrRedeem == "redeem" {
						actor = side // this client
					} else if side == order.Maker {
						actor = order.Taker // counter-party
					} else {
						actor = order.Maker
					}
					client.log.Infof("Mined %d %s blocks for %s's %s, match %s", nBlocks, unbip(assetID),
						actor, swapOrRedeem, token(match.MatchID.Bytes()))
				} else {
					client.log.Infof("%s mine error %v", unbip(assetID), err) // return err???
				}
			}
		}
		return completedTrades == len(tracker.matches)
	})
	if s.ctx.Err() != nil { // context canceled
		return nil
	}

	var incompleteTrades int
	tracker.mtx.RLock()
	for _, match := range tracker.matches {
		if match.Status < finalStatus {
			incompleteTrades++
			client.log.Infof("incomplete trade: order %s, match %s, status %s, side %s", tracker.token(),
				token(match.MatchID[:]), match.Status, match.Side)
		} else {
			client.log.Infof("trade for order %s, match %s monitored successfully till %s, side %s", tracker.token(),
				token(match.MatchID[:]), match.Status, match.Side)
		}
	}
	tracker.mtx.RUnlock()
	if incompleteTrades > 0 {
		return fmt.Errorf("client %s reported %d incomplete trades for order %s after %s",
			client.name, incompleteTrades, tracker.token(), maxTradeDuration)
	}

	return nil
}

func (s *simulationTest) checkAndWaitForRefunds(ctx context.Context, client *simulationClient, orderID string) error {
	// check if client has pending refunds
	client.log.Infof("checking if refunds are necessary")
	refundAmts := map[uint32]int64{s.base.id: 0, s.quote.id: 0}
	var furthestLockTime time.Time

	hasRefundableSwap := func(match *matchTracker) bool {
		sentSwap := match.MetaData.Proof.ContractData != nil
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
		client.log.Infof("No refunds necessary")
		return nil
	}

	client.log.Infof("Found refundable swaps worth %s %s and %s %s", s.base.valFmt(refundAmts[s.base.id]),
		s.base.symbol, s.quote.valFmt(refundAmts[s.quote.id]), s.quote.symbol)

	// wait for refunds to be executed
	now := time.Now()
	if furthestLockTime.After(now) {
		wait := furthestLockTime.Sub(now)
		client.log.Infof("Waiting %v before checking wallet balances for expected refunds", wait)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}

	// swaps cannot be refunded until the MedianTimePast is greater than
	// the swap locktime. The MedianTimePast is calculated by taking the
	// timestamps of the last 11 blocks and finding the median. Mining 6
	// blocks on the chain a second from now will ensure that the
	// MedianTimePast will be greater than the furthest swap locktime,
	// thereby lifting the time lock on all these swaps.
	mineMedian := func(assetID uint32) error {
		time.Sleep(sleepFactor * time.Second)
		if err := newHarnessCtrl(assetID).mineBlocks(ctx, 6); err != nil {
			return fmt.Errorf("client %s: error mining 6 %s blocks for swap refunds: %v",
				client.name, unbip(assetID), err)
		}
		client.log.Infof("Mined 6 blocks for assetID %d to expire swap locktimes", assetID)
		return nil
	}
	if refundAmts[s.quote.id] > 0 {
		if err := mineMedian(s.quote.id); err != nil {
			return err
		}
	}
	if refundAmts[s.base.id] > 0 {
		if err := mineMedian(s.base.id); err != nil {
			return err
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
		return fmt.Errorf("client %s reported %d unrefunded swaps after %s",
			client.name, notRefundedSwaps, refundWaitTimeout)
	}

	// swaps refunded, mine some blocks to get the refund txs confirmed and
	// confirm that balance changes are as expected.
	for assetID, expectedBalanceDiff := range refundAmts {
		if expectedBalanceDiff > 0 {
			if err := newHarnessCtrl(assetID).mineBlocks(ctx, 1); err != nil {
				return fmt.Errorf("%s mine error %v", unbip(assetID), err)
			}
		}
	}
	time.Sleep(4 * sleepFactor * time.Second)

	client.expectBalanceDiffs = refundAmts
	err = s.assertBalanceChanges(client)
	if err == nil {
		client.log.Infof("successfully refunded swaps worth %s %s and %s %s", s.base.valFmt(refundAmts[s.base.id]),
			s.base.symbol, s.quote.valFmt(refundAmts[s.quote.id]))
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

// marketConfig taken from dcrdex/server/cmd/dcrdex/settings.go
type marketConfig struct {
	Markets []*struct {
		Base           string  `json:"base"`
		Quote          string  `json:"quote"`
		LotSize        uint64  `json:"lotSize"`
		RateStep       uint64  `json:"rateStep"`
		Duration       uint64  `json:"epochDuration"`
		MBBuffer       float64 `json:"marketBuyBuffer"`
		BookedLotLimit uint32  `json:"userBookedLotLimit"`
	} `json:"markets"`
	Assets map[string]*dexsrv.AssetConf `json:"assets"`
}

type harnessCtrl struct {
	dir, fundStr    string
	blockMultiplier uint32
}

func (hc *harnessCtrl) run(ctx context.Context, cmd string, args ...string) error {
	command := exec.CommandContext(ctx, cmd, args...)
	command.Dir = hc.dir
	return command.Run()
}

func (hc *harnessCtrl) mineBlocks(ctx context.Context, n uint32) error {
	n *= hc.blockMultiplier
	return hc.run(ctx, "./mine-alpha", fmt.Sprintf("%d", n))
}

func (hc *harnessCtrl) fund(ctx context.Context, address string, amts []int) error {
	for _, amt := range amts {
		fs := fmt.Sprintf(hc.fundStr, address, amt)
		strs := strings.Split(fs, "_")
		err := hc.run(ctx, "./alpha", strs...)
		if err != nil {
			return err
		}
	}
	return nil
}

func newHarnessCtrl(assetID uint32) *harnessCtrl {
	var (
		fundStr                = "sendtoaddress_%s_%d"
		blockMultiplier uint32 = 1
	)
	switch assetID {
	case dcr.BipID, btc.BipID, ltc.BipID, bch.BipID, doge.BipID:
	case eth.BipID:
		// Sending with values of .1 eth.
		fundStr = `attach_--exec personal.sendTransaction({from:eth.accounts[1],to:"%s",gasPrice:200e9,value:%de17},"abc")`
		blockMultiplier = 4
	default:
		panic(fmt.Sprintf("unknown asset %d for harness control", assetID))
	}
	dir := filepath.Join(dextestDir, dex.BipIDSymbol(assetID), "harness-ctl")
	return &harnessCtrl{
		dir:             dir,
		fundStr:         fundStr,
		blockMultiplier: blockMultiplier,
	}
}

type tWallet struct {
	pass       []byte
	config     map[string]string
	walletType string // type is a keyword
	fund       bool
	hc         *harnessCtrl
}

func dcrWallet(node string) (*tWallet, error) {
	cfg, err := config.Parse(filepath.Join(dextestDir, "dcr", node, fmt.Sprintf("%s.conf", node)))
	if err != nil {
		return nil, err
	}
	cfg["account"] = "default"
	return &tWallet{
		walletType: "dcrwalletRPC",
		pass:       []byte("abc"),
		config:     cfg,
	}, nil
}

func btcWallet(useSPV bool, node string) (*tWallet, error) {
	return btcCloneWallet(btc.BipID, useSPV, node, "bitcoindRPC")
}

func ltcWallet(node string) (*tWallet, error) {
	return btcCloneWallet(ltc.BipID, false, node, "litecoindRPC")
}

func bchWallet(useSPV bool, node string) (*tWallet, error) {
	return btcCloneWallet(bch.BipID, useSPV, node, "bitcoindRPC")
}

func ethWallet() (*tWallet, error) {
	return &tWallet{
		fund:       true,
		walletType: "geth",
	}, nil
}

func btcCloneWallet(assetID uint32, useSPV bool, node string, rpcWalletType string) (*tWallet, error) {
	if useSPV {
		return &tWallet{
			walletType: "SPV",
			fund:       true,
		}, nil
	}

	parentNode := node
	pass := []byte("abc")
	switch node {
	case "gamma":
		pass = nil
	case "delta":
		pass = nil
	}

	switch assetID {
	case doge.BipID: // , zec.BipID:
	// dogecoind doesn't support > 1 wallet, so gamma and delta
	// have their own nodes.
	default:
		switch node {
		case "gamma":
			parentNode = "alpha"
		case "delta":
			parentNode = "beta"
		}
	}

	cfg, err := config.Parse(filepath.Join(dextestDir, dex.BipIDSymbol(assetID), parentNode, parentNode+".conf"))
	if err != nil {
		return nil, err
	}

	if parentNode != node {
		cfg["walletname"] = node
	}

	return &tWallet{
		walletType: rpcWalletType,
		pass:       pass,
		config:     cfg,
	}, nil
}

func dogeWallet(node string) (*tWallet, error) {
	return btcCloneWallet(doge.BipID, false, node, "dogecoindRPC")
}

func (s *simulationTest) newClient(name string, cl *SimClient) (*simulationClient, error) {
	wallets := make(map[uint32]*tWallet, 2)
	addWallet := func(assetID uint32, useSPV bool, node string) error {
		var (
			tw  *tWallet
			err error
		)
		switch assetID {
		case dcr.BipID:
			tw, err = dcrWallet(node)
		case btc.BipID:
			tw, err = btcWallet(useSPV, node)
		case eth.BipID:
			tw, err = ethWallet()
		case ltc.BipID:
			tw, err = ltcWallet(node)
		case bch.BipID:
			tw, err = bchWallet(useSPV, node)
		case doge.BipID:
			tw, err = dogeWallet(node)
		default:
			return fmt.Errorf("no method to create wallet for asset %d", assetID)
		}
		if err != nil {
			return err
		}
		wallets[assetID] = tw
		return nil
	}
	if err := addWallet(s.base.id, cl.BaseSPV, cl.BaseNode); err != nil {
		return nil, err
	}
	if err := addWallet(s.quote.id, cl.QuoteSPV, cl.QuoteNode); err != nil {
		return nil, err
	}
	return &simulationClient{
		name:            name,
		log:             s.log.SubLogger(name),
		appPass:         []byte(fmt.Sprintf("client-%s", name)),
		wallets:         wallets,
		processedStatus: make(map[order.MatchID]order.MatchStatus),
	}, nil

}

type simulationClient struct {
	*SimClient
	name         string
	log          dex.Logger
	wg           sync.WaitGroup
	core         *Core
	notes        *notificationReader
	noRedeemWait bool

	psMTX           sync.Mutex
	processedStatus map[order.MatchID]order.MatchStatus

	appPass  []byte
	wallets  map[uint32]*tWallet
	balances map[uint32]uint64
	isSeller bool
	// Update after each test run to perform post-test balance
	// change validation. Set to nil to NOT perform balance checks.
	expectBalanceDiffs map[uint32]int64
	lastOrder          []byte
}

var clientCounter uint32

func (client *simulationClient) init(ctx context.Context) error {
	tmpDir, _ := os.MkdirTemp("", "")
	cNum := atomic.AddUint32(&clientCounter, 1)
	var err error
	client.core, err = New(&Config{
		DBPath: filepath.Join(tmpDir, "dex.db"),
		Net:    dex.Regtest,
		Logger: dex.StdOutLogger("CORE:"+strconv.Itoa(int(cNum)), client.log.Level()),
	})
	if err != nil {
		return err
	}

	client.core.lockTimeTaker = tLockTimeTaker
	client.core.lockTimeMaker = tLockTimeMaker
	client.notes = client.startNotificationReader(ctx)
	return nil
}

func (s *simulationTest) registerDEX(client *simulationClient) error {
	dc := client.dc()
	if dc != nil {
		dc.connMaster.Disconnect()
		client.core.connMtx.Lock()
		delete(client.core.conns, dc.acct.host)
		client.core.connMtx.Unlock()
	}

	dexConf, err := client.core.GetDEXConfig(dexHost, nil)
	if err != nil {
		return err
	}

	feeAssetSymbol := dex.BipIDSymbol(s.regAsset)

	feeAsset := dexConf.RegFees[feeAssetSymbol]
	if feeAsset == nil {
		return fmt.Errorf("%s not supported for fees!", feeAssetSymbol)
	}
	dexFee := feeAsset.Amt

	// connect dex and pay fee
	regRes, err := client.core.Register(&RegisterForm{
		Addr:    dexHost,
		AppPass: client.appPass,
		Fee:     dexFee,
		Asset:   &s.regAsset,
	})
	if err != nil {
		return err
	}
	client.log.Infof("Sent registration fee to DEX %s", dexHost)

	// Mine block(s) to mark fee as paid.

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-time.After(time.Second * 5):
				err = newHarnessCtrl(s.regAsset).mineBlocks(s.ctx, 1)
				if err != nil {
					client.log.Errorf("error mining fee blocks: %v", err)
				}
			case <-done:
				return
			}
		}
	}()

	client.log.Infof("Mined %d %s blocks for fee payment confirmation", regRes.ReqConfirms, feeAssetSymbol)

	// Wait up to bTimeout+12 seconds for fee payment. notify_fee times out
	// after bTimeout+10 seconds.
	feeTimeout := time.Millisecond*time.Duration(client.dc().cfg.BroadcastTimeout) + 12*time.Second
	client.log.Infof("Waiting %v for fee confirmation notice", feeTimeout)
	feePaid := client.notes.find(s.ctx, feeTimeout, func(n Notification) bool {
		return n.Type() == NoteTypeFeePayment && n.Topic() == TopicAccountRegistered
	})
	close(done)
	if !feePaid {
		return fmt.Errorf("fee payment not confirmed after %s", feeTimeout)
	}

	client.log.Infof("Fee payment confirmed")
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
func (client *simulationClient) startNotificationReader(ctx context.Context) *notificationReader {
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

func (s *simulationTest) placeOrder(client *simulationClient, qty, rate uint64, tifNow bool) (string, error) {
	dc := client.dc()
	mkt := dc.marketConfig(s.marketName)
	if mkt == nil {
		return "", fmt.Errorf("no %s market found", s.marketName)
	}

	tradeForm := &TradeForm{
		Host:    dexHost,
		Base:    s.base.id,
		Quote:   s.quote.id,
		IsLimit: true,
		Sell:    client.isSeller,
		Qty:     qty,
		Rate:    rate,
		TifNow:  tifNow,
	}

	ord, err := client.core.Trade(client.appPass, tradeForm)
	if err != nil {
		return "", err
	}

	client.lastOrder = ord.ID

	r := calc.ConventionalRateAlt(rate, s.base.conversionFactor, s.quote.conversionFactor)

	client.log.Infof("placed order %sing %s %s at %s %s (%s)", sellString(client.isSeller),
		s.base.valFmt(qty), s.base.symbol, r, s.quote.symbol, ord.ID[:8])
	return ord.ID.String(), nil
}

func (s *simulationTest) updateBalances(client *simulationClient) error {
	client.log.Infof("updating balances")
	client.balances = make(map[uint32]uint64, len(client.wallets))
	setBalance := func(a *assetConfig) error {
		balances, err := client.core.AssetBalance(a.id)
		if err != nil {
			return err
		}
		client.balances[a.id] = balances.Available + balances.Immature + balances.Locked
		client.log.Infof("%s available %s, immature %s, locked %s", a.symbol,
			a.valFmt(balances.Available), a.valFmt(balances.Immature), a.valFmt(balances.Locked))
		return nil
	}
	if err := setBalance(s.base); err != nil {
		return err
	}
	return setBalance(s.quote)
}

func (s *simulationTest) assertBalanceChanges(client *simulationClient) error {
	if client.expectBalanceDiffs == nil {
		return errors.New("balance diff is nil")
	}

	ord, err := client.core.Order(client.lastOrder)
	if err != nil {
		return errors.New("last order not found")
	}

	var baseFees, quoteFees int64
	if fees := ord.FeesPaid; fees != nil {
		if ord.Sell {
			baseFees = int64(fees.Swap)
			quoteFees = int64(fees.Redemption)
		} else {
			quoteFees = int64(fees.Swap)
			baseFees = int64(fees.Redemption)
		}
	}

	defer func() {
		// Clear after assertion so that the next assertion is only performed
		// if the expected balance changes are explicitly set.
		client.expectBalanceDiffs = nil
	}()
	prevBalances := client.balances
	err = s.updateBalances(client)
	if err != nil {
		return err
	}

	checkDiff := func(a *assetConfig, expDiff, fees int64) error {
		// actual diff will likely be less than expected because of tx fees
		// TODO: account for actual fee(s) or use a more realistic fee estimate.
		expVal := expDiff - fees
		minExpectedDiff, maxExpectedDiff := int64(float64(expVal)*0.95), int64(float64(expVal)*1.05)

		// diffs can be negative
		if minExpectedDiff > maxExpectedDiff {
			minExpectedDiff, maxExpectedDiff = maxExpectedDiff, minExpectedDiff
		}

		balanceDiff := int64(client.balances[a.id]) - int64(prevBalances[a.id])
		if balanceDiff < minExpectedDiff || balanceDiff > maxExpectedDiff {
			// precisionStr := fmt.Sprintf(,
			// 	math.Log10(float64(conversionFactor)))
			return fmt.Errorf("%s balance change not in expected range %s - %s, got %s", a.symbol,
				a.valFmt(minExpectedDiff), a.valFmt(maxExpectedDiff), a.valFmt(balanceDiff))
		}
		client.log.Infof("%s balance change %s is in expected range of %s - %s", a.symbol,
			a.valFmt(balanceDiff), a.valFmt(minExpectedDiff), a.valFmt(maxExpectedDiff))
		return nil
	}

	if err := checkDiff(s.base, client.expectBalanceDiffs[s.base.id], baseFees); err != nil {
		return err
	}
	return checkDiff(s.quote, client.expectBalanceDiffs[s.quote.id], quoteFees)
}

func (client *simulationClient) dc() *dexConnection {
	client.core.connMtx.RLock()
	defer client.core.connMtx.RUnlock()
	return client.core.conns[dexHost]
}

func (client *simulationClient) findOrder(orderID string) (*trackedTrade, error) {
	oid, err := order.IDFromHex(orderID)
	if err != nil {
		return nil, fmt.Errorf("error parsing order id %s -> %v", orderID, err)
	}
	tracker, _, _ := client.dc().findOrder(oid)
	return tracker, nil
}

// Torpedo the client's wallets by forcing (*Core).locallyUnlocked to return
// false, even for an unencrypted wallet.
func (client *simulationClient) disableWallets() {
	client.log.Infof("Torpedoing wallets")
	client.core.walletMtx.Lock()
	// NOTE: this is not reversible, but could be made so with the undo data:
	// walletPasses[cid] = make(map[uint32]passes, len(client.core.wallets))
	for _, wallet := range client.core.wallets {
		wallet.mtx.Lock()
		wallet.encPass = []byte{0}
		wallet.pw = nil
		wallet.mtx.Unlock()
	}
	client.core.walletMtx.Unlock()
}

func (client *simulationClient) disconnectWallets() {
	client.log.Infof("Disconnecting wallets")
	client.core.walletMtx.Lock()
	for _, wallet := range client.core.wallets {
		wallet.Disconnect()
	}
	client.core.walletMtx.Unlock()
}

func init() {
	if race {
		sleepFactor = 3
	}
}
