//go:build harness && !nolgpl

package core

// The asset and dcrdex harnesses should be running before executing this test.
//
// The ./run script rebuilds the dcrdex binary with dex.testLockTimeTaker=1m
// and dex.testLockTimeMaker=2m before running the binary, making it possible
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
	"encoding/json"
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
	"decred.org/dcrdex/client/asset/base"
	"decred.org/dcrdex/client/asset/bch"
	"decred.org/dcrdex/client/asset/btc"
	"decred.org/dcrdex/client/asset/dash"
	"decred.org/dcrdex/client/asset/dcr"
	"decred.org/dcrdex/client/asset/dgb"
	"decred.org/dcrdex/client/asset/doge"
	"decred.org/dcrdex/client/asset/eth"
	"decred.org/dcrdex/client/asset/firo"
	"decred.org/dcrdex/client/asset/ltc"
	"decred.org/dcrdex/client/asset/polygon"
	"decred.org/dcrdex/client/asset/zcl"
	"decred.org/dcrdex/client/asset/zec"
	"decred.org/dcrdex/client/comms"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/config"
	"decred.org/dcrdex/dex/msgjson"
	dexbtc "decred.org/dcrdex/dex/networks/btc"
	dexdgb "decred.org/dcrdex/dex/networks/dgb"
	dexdoge "decred.org/dcrdex/dex/networks/doge"
	dexeth "decred.org/dcrdex/dex/networks/eth"
	"decred.org/dcrdex/dex/order"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"golang.org/x/sync/errgroup"
)

var (
	dexHost = "127.0.0.1:17273"

	homeDir    = os.Getenv("HOME")
	dextestDir = filepath.Join(homeDir, "dextest")
	// Use websocket endpoints for EVM harnesses. The harness scripts
	// start geth with --ws on these ports. IPC is not available.
	ethAlphaWSAddr     = "ws://localhost:38557"
	polygonAlphaWSAddr = "ws://localhost:34983"
	baseAlphaWSAddr    = "ws://localhost:39557"

	ethUsdcID, _     = dex.BipSymbolID("usdc.eth")
	ethUsdtID, _     = dex.BipSymbolID("usdt.eth")
	polygonUsdcID, _ = dex.BipSymbolID("usdc.polygon")
	polygonUsdtID, _ = dex.BipSymbolID("usdt.polygon")
	baseUsdcID, _    = dex.BipSymbolID("usdc.base")
	baseUsdtID, _    = dex.BipSymbolID("usdt.base")
)

type SimWalletType int

const (
	WTCoreClone SimWalletType = iota + 1
	WTSPVNative
	WTElectrum
)

// SimClient is the configuration for the client's wallets.
type SimClient struct {
	BaseWalletType  SimWalletType
	QuoteWalletType SimWalletType
	BaseNode        string
	QuoteNode       string
}

type assetConfig struct {
	id               uint32
	symbol           string
	conversionFactor uint64
	valFmt           func(any) string
	isToken          bool
}

var testLookup = map[string]func(s *simulationTest) error{
	"success":       testTradeSuccess,
	"nomakerswap":   testNoMakerSwap,
	"notakerswap":   testNoTakerSwap,
	"nomakerredeem": testNoMakerRedeem,
	"makerghost":    testMakerGhostingAfterTakerRedeem,
	"orderstatus":   testOrderStatusReconciliation,
	"resendpending": testResendPendingRequests,
	"notakeraddr":   testNoTakerAddr,
	"nomakeraddr":   testNoMakerAddr,
	"notakerredeem": testNoTakerRedeem,
	"missedcpaddr":  testMissedCounterPartyAddr,
}

func SimTests() []string {
	// Use this slice instead of the generating from the testLookup map so that
	// the order doesn't change.
	return []string{
		"success",
		"nomakerswap",
		"notakerswap",
		"nomakerredeem",
		"makerghost",
		"orderstatus",
		"resendpending",
		"notakeraddr",
		"nomakeraddr",
		"notakerredeem",
		"missedcpaddr",
	}
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
	RunOnce           bool
}

type simulationTest struct {
	ctx            context.Context
	cancel         context.CancelFunc
	log            dex.Logger
	base           *assetConfig
	quote          *assetConfig
	regAsset       uint32
	marketName     string
	lotSize        uint64
	rateStep       uint64
	minRate        uint64
	client1        *simulationClient
	client2        *simulationClient
	clients        []*simulationClient
	client1IsMaker bool
}

// clampRate ensures a rate is at least the market's minimum rate,
// rounded up to the nearest rateStep.
func (s *simulationTest) clampRate(rate uint64) uint64 {
	if rate >= s.minRate {
		return rate
	}
	// Round up to nearest rateStep.
	return ((s.minRate + s.rateStep - 1) / s.rateStep) * s.rateStep
}

func (s *simulationTest) waitALittleBit() {
	sleep := 4 * time.Second
	if s.client1.BaseWalletType == WTElectrum || s.client1.QuoteWalletType == WTElectrum ||
		s.client2.BaseWalletType == WTElectrum || s.client2.QuoteWalletType == WTElectrum {
		sleep = 7 * time.Second
	}
	s.log.Infof("Waiting a little bit, %s.", sleep)
	time.Sleep(sleep * sleepFactor)
}

// flushPendingTxs mines blocks on both assets and waits briefly to confirm
// any pending transactions (e.g. refunds) from previous tests, preventing
// balance contamination in the next test's assertions.
func (s *simulationTest) flushPendingTxs() {
	ctx := s.ctx
	for _, a := range []*assetConfig{s.base, s.quote} {
		hctrl := newHarnessCtrl(a.id)
		if err := hctrl.mineBlocks(ctx, 2); err != nil {
			s.log.Warnf("Error mining %s blocks during flush: %v", a.symbol, err)
		}
	}
	time.Sleep(2 * time.Second * sleepFactor)
}

// RunSimulationTest runs one or more simulations tests, based on the provided
// SimulationConfig.
func RunSimulationTest(cfg *SimulationConfig) error {
	if cfg.Client1.BaseWalletType == WTCoreClone && cfg.Client2.BaseWalletType == WTCoreClone &&
		cfg.Client1.BaseNode == cfg.Client2.BaseNode {
		return fmt.Errorf("the %s RPC wallets for both clients are the same", cfg.BaseSymbol)
	}

	if cfg.Client1.QuoteWalletType == WTCoreClone && cfg.Client2.QuoteWalletType == WTCoreClone &&
		cfg.Client1.QuoteNode == cfg.Client2.QuoteNode {
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
	valFormatter := func(valFmt func(uint64) string) func(any) string {
		return func(vi any) string {
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
			isToken:          asset.TokenInfo(baseID) != nil,
		},
		quote: &assetConfig{
			id:               quoteID,
			symbol:           cfg.QuoteSymbol,
			conversionFactor: quoteUnitInfo.Conventional.ConversionFactor,
			valFmt:           valFormatter(quoteUnitInfo.ConventionalString),
			isToken:          asset.TokenInfo(quoteID) != nil,
		},
		regAsset:   regAsset,
		marketName: marketName(baseID, quoteID),
	}

	if err := s.setup(cfg.Client1, cfg.Client2); err != nil {
		return fmt.Errorf("setup error: %w", err)
	}
	spacer := `

*******************************************************************************
`
	for _, testName := range cfg.Tests {
		// Mine blocks on both assets to confirm any pending txs from
		// previous tests before starting the next one.
		s.flushPendingTxs()
		s.log.Infof("%s\nRunning test %q with client 1 as maker on market %q.%s", spacer, testName, s.marketName, spacer)
		f, ok := testLookup[testName]
		if !ok {
			return fmt.Errorf("no test named %q", testName)
		}
		s.client1IsMaker = true
		if err := f(s); err != nil {
			return fmt.Errorf("test %q failed with client 1 as maker: %w", testName, err)
		}
		s.log.Infof("%s\nSUCCESS!! Test %q with client 1 as maker on market %q PASSED!%s", spacer, testName, s.marketName, spacer)
		s.client1IsMaker = false
		if !cfg.RunOnce && testName != "orderstatus" && testName != "missedcpaddr" {
			s.flushPendingTxs()
			s.log.Infof("%s\nRunning test %q with client 2 as maker on market %q.%s", spacer, testName, s.marketName, spacer)
			if err := f(s); err != nil {
				return fmt.Errorf("test %q failed with client 2 as maker: %w", testName, err)
			}
			s.log.Infof("%s\nSUCCESS!! Test %q with client 2 as maker on market %q PASSED!%s", spacer, testName, s.marketName, spacer)
		}
	}

	s.cancel()
	for _, c := range s.clients {
		c.wg.Wait()
		c.log.Infof("Client %s done.", c.name)
	}

	return nil
}

func (s *simulationTest) startClients() error {
	for _, c := range s.clients {
		err := c.init(s.ctx)
		if err != nil {
			return err
		}
		walletNames := [2]string{}
		var i int
		for bipID := range c.wallets {
			walletNames[i] = unbip(bipID)
			i++
		}
		c.log.Infof("Setting up client %s with wallets %s and %s.", c.name, walletNames[0], walletNames[1])

		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			c.core.Run(s.ctx)
		}()
		<-c.core.Ready()

		// init app
		_, err = c.core.InitializeClient(c.appPass, nil)
		if err != nil {
			return err
		}
		c.log.Infof("Core initialized.")

		c.core.Login(c.appPass)

		createWallet := func(pass []byte, fund bool, form *WalletForm) error {
			err = c.core.CreateWallet(c.appPass, pass, form)
			if err != nil {
				// Creating a parent wallet (e.g. eth) auto-creates its
				// token wallets. Tolerate "already exists" so we still
				// fund and approve the token below.
				if c.core.WalletState(form.AssetID) != nil {
					c.log.Infof("%s wallet already exists, skipping creation.", unbip(form.AssetID))
				} else {
					return err
				}
			}
			c.log.Infof("Connected %s wallet (fund = %v).", unbip(form.AssetID), fund)
			hctrl := newHarnessCtrl(form.AssetID)
			// eth needs the headers to be new in order to
			// count itself synced, so mining a few blocks here.
			hctrl.mineBlocks(s.ctx, 1)
			c.log.Infof("Waiting for %s wallet to sync.", unbip(form.AssetID))
			synced := make(chan error)
			go func() {
				tStart := time.Now()
				for time.Since(tStart) < time.Second*30 {
					if c.core.WalletState(form.AssetID).Synced {
						synced <- nil
						return
					}
					time.Sleep(time.Second)
				}
				synced <- fmt.Errorf("wallet never synced")
			}()
			if err := <-synced; err != nil {
				return fmt.Errorf("wallet never synced")
			}
			c.log.Infof("Client %s %s wallet.", c.name, unbip(form.AssetID))
			if fund {
				// Fund new wallet.
				c.log.Infof("Client %s funding synced %s wallet.", c.name, unbip(form.AssetID))
				address := c.core.WalletState(form.AssetID).Address
				amts := []int{10, 18, 5, 7, 1, 15, 3, 25, 20, 20}
				if err := hctrl.fund(s.ctx, address, amts); err != nil {
					return fmt.Errorf("fund error: %w", err)
				}
			mined:
				for fundTimeout := time.After(20 * time.Second); ; {
					hctrl.mineBlocks(s.ctx, 2)
					bal, err := c.core.AssetBalance(form.AssetID)
					if err != nil {
						return fmt.Errorf("error getting balance for %s: %w", unbip(form.AssetID), err)
					}
					if bal.Available > 0 {
						break mined
					}
					s.log.Infof("Waiting for %s funding tx to be mined", unbip(form.AssetID))
					select {
					case <-fundTimeout:
						return fmt.Errorf("timed out waiting for %s funding tx to be mined", unbip(form.AssetID))
					case <-time.After(time.Second * 2):
					case <-s.ctx.Done():
						return s.ctx.Err()
					}
				}

				// Tip change after block filtering scan takes the wallet time.
				time.Sleep(2 * time.Second * sleepFactor)

				w, _ := c.core.wallet(form.AssetID)
				approved := make(chan struct{})
				if approver, is := w.Wallet.(asset.TokenApprover); is {
					const contractVer = 1
					if _, err := approver.ApproveToken(contractVer, func() {
						close(approved)
					}); err != nil {
						return fmt.Errorf("error approving %s token: %w", unbip(form.AssetID), err)
					}
				out:
					for {
						s.log.Infof("Mining more blocks to get approval tx confirmed")
						hctrl.mineBlocks(s.ctx, 2)
						select {
						case <-approved:
							c.log.Infof("%s approved", unbip(form.AssetID))
							break out
						case <-time.After(time.Second * 5):
						case <-s.ctx.Done():
							return s.ctx.Err()
						}
					}
				}
			}

			return nil
		}

		// connect wallets
		for assetID, wallet := range c.wallets {
			os.RemoveAll(c.core.assetDataDirectory(assetID))

			c.log.Infof("Creating %s %s-type wallet. config = %+v.", dex.BipIDSymbol(assetID), wallet.walletType, wallet.config)

			pw := wallet.pass

			if wallet.parent != nil {
				if err := createWallet(pw, wallet.fund, wallet.parent); err != nil {
					return fmt.Errorf("error creating parent %s wallet: %w", dex.BipIDSymbol(wallet.parent.AssetID), err)
				}
				// For degen assets, the pass is only for the parent.
				pw = nil
			}

			if err := createWallet(pw, wallet.fund, &WalletForm{
				AssetID: assetID,
				Config:  wallet.config,
				Type:    wallet.walletType,
			}); err != nil {
				return err
			}

		}

		// Tip change after block filtering scan takes the wallet time, even
		// longer for Electrum.
		sleep := 2 * time.Second
		if c.BaseWalletType == WTElectrum || c.QuoteWalletType == WTElectrum {
			sleep = 6 * time.Second
		}
		time.Sleep(sleep * sleepFactor)

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

	if mktCfg == nil {
		return fmt.Errorf("market %s not found", s.marketName)
	}

	s.lotSize = mktCfg.LotSize
	s.rateStep = mktCfg.RateStep

	quoteAsset := dc.assetConfig(mktCfg.Quote)
	if quoteAsset != nil {
		s.minRate = dc.minimumMarketRate(quoteAsset, mktCfg.LotSize)
	}

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
	s.client1.replaceConns()
	s.client2.replaceConns()
	return nil
}

var sleepFactor time.Duration = 1

// TestTradeSuccess runs a simple trade test and ensures that the resulting
// trades are completed successfully.
func testTradeSuccess(s *simulationTest) error {
	var qty, rate uint64 = 2 * s.lotSize, s.clampRate(150 * s.rateStep)
	s.client1.isSeller, s.client2.isSeller = true, false
	return s.simpleTradeTest(qty, rate, order.MatchConfirmed)
}

// TestNoMakerSwap runs a simple trade test and ensures that the resulting
// trades fail because of the Maker not sending their init swap tx.
func testNoMakerSwap(s *simulationTest) error {
	var qty, rate uint64 = 1 * s.lotSize, s.clampRate(100 * s.rateStep)
	s.client1.isSeller, s.client2.isSeller = false, true
	return s.simpleTradeTest(qty, rate, order.NewlyMatched)
}

// TestNoTakerSwap runs a simple trade test and ensures that the resulting
// trades fail because of the Taker not sending their init swap tx.
// Also ensures that Maker's funds are refunded after locktime expires.
func testNoTakerSwap(s *simulationTest) error {
	var qty, rate uint64 = 3 * s.lotSize, s.clampRate(200 * s.rateStep)
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
	var qty, rate uint64 = 1 * s.lotSize, s.clampRate(250 * s.rateStep)
	s.client1.isSeller, s.client2.isSeller = true, false

	enable := func(client *simulationClient) {
		client.enableWallets()
		s.client1.filteredConn.requestFilter.Store(func(string) error { return nil })
		s.client2.filteredConn.requestFilter.Store(func(string) error { return nil })
	}
	var killed uint32
	preFilter1 := func(route string) error {
		if route == msgjson.InitRoute && atomic.CompareAndSwapUint32(&killed, 0, 1) {
			s.client1.disableWallets()
			time.AfterFunc(s.client1.core.lockTimeTaker, func() { enable(s.client1) })
		}
		return nil
	}
	preFilter2 := func(route string) error {
		if route == msgjson.InitRoute && atomic.CompareAndSwapUint32(&killed, 0, 1) {
			s.client2.disableWallets()
			time.AfterFunc(s.client1.core.lockTimeTaker, func() { enable(s.client2) })
		}
		return nil
	}

	s.client1.filteredConn.requestFilter.Store(preFilter1)
	s.client2.filteredConn.requestFilter.Store(preFilter2)

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
	var qty, rate uint64 = 1 * s.lotSize, s.clampRate(250 * s.rateStep)
	s.client1.isSeller, s.client2.isSeller = true, false

	var bits uint8
	anErr := errors.New("intentional error from test")
	// Prevent the first redeemer, who must be maker, from sending
	// redeem info to the server.
	preFilter1 := func(route string) error {
		if route == msgjson.RedeemRoute {
			if bits == 0 {
				bits = 0b1
			}
			if bits&0b1 != 0 {
				return anErr
			}
		}
		return nil
	}
	preFilter2 := func(route string) error {
		if route == msgjson.RedeemRoute {
			if bits == 0 {
				bits = 0b10
			}
			if bits&0b10 != 0 {
				return anErr
			}
		}
		return nil
	}

	defer s.client1.filteredConn.withRequestFilter(preFilter1)()
	defer s.client2.filteredConn.withRequestFilter(preFilter2)()

	return s.simpleTradeTest(qty, rate, order.MatchConfirmed)
}

// TestOrderStatusReconciliation simulates a few conditions that could cause a
// client to record a wrong status for an order, especially where the client
// considers an order as active when it no longer is. The expectation is that
// the client can infer the correct order status for such orders and update
// accordingly. The following scenarios are simulated and tested:
//
// Order 1:
//   - Standing order, preimage not revealed, "missed" revoke_order note.
//   - Expect order status to stay at Epoch status before going AWOL and to
//     become Revoked after re-connecting the DEX. Locked coins should be
//     returned.
//
// Order 2:
//   - Non-standing order, preimage revealed, "missed" nomatch or match request
//     (if matched).
//   - Expect order status to stay at Epoch status before going AWOL and to
//     become Executed after re-connecting the DEX, even if the order was
//     matched and the matches got revoked due to client inaction.
//
// Order 3:
//   - Standing order, partially matched, booked, revoked due to inaction on a
//     match.
//   - Expect order status to be Booked before going AWOL and to become Revoked
//     after re-connecting the DEX. Locked coins should be returned.
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
	s.log.Infof("Client 2 %s available balance is %v.", s.base.symbol, s.base.valFmt(c2Balance.Available))

	rate := s.clampRate(100 * s.rateStep)

	s.log.Infof("%s\n", `
Placing an order for client 1, qty=3*lotSize, rate=100*rateStep
This order should get matched to either or both of these client 2
sell orders:
- Order 2: immediate limit order, qty=2*lotSize, rate=100*rateStep,
           may not get matched if Order 3 below is matched first.
- Order 3: standing limit order, qty=4*lotSize, rate=100*rateStep,
           will always be partially matched (3*lotSize matched or
           1*lotSize matched, if Order 2 is matched first).`)
	waiter.Go(func() error {
		_, err := s.placeOrder(s.client1, 3*s.lotSize, rate, false)
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
		s.log.Infof("Forcing client 2 to forget order %s.", oid)
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

	s.log.Infof("%s\n", `
Placing Client 2 Order 1:
 - Standing order, preimage not revealed, "missed" revoke_order note.
 - Expect order status to stay at Epoch status before going AWOL and
   to become Revoked after re-connecting the DEX. Locked coins should
   be returned.`)

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

	s.log.Infof("%s\n", `
Placing Client 2 Order 2:
- Non-standing order, preimage revealed, "missed" nomatch or match
  request (if matched).
- Expect order status to stay at Epoch status before going AWOL and
  to become Executed after re-connecting the DEX, even if the order
  was matched and the matches got revoked due to client inaction. No
  attempt is made to cause match revocation anyways.`)

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
		twoEpochs := 2 * time.Duration(tracker.epochLen()) * time.Millisecond
		s.client2.log.Infof("Client 2 waiting %v for preimage reveal, order %s", twoEpochs, tracker.token())
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

	s.log.Infof("%s\n", `
Client 2 placing Order 3:
 - Standing order, partially matched, booked, revoked due to inaction on
   a match.
 - Expect order status to be Booked before going AWOL and to become
   Revoked after re-connecting the DEX. Locked coins should be returned.`)

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
		twoEpochs := 2 * time.Duration(tracker.epochLen()) * time.Millisecond
		s.client2.log.Infof("Client 2 waiting %v for preimage reveal, order %s", twoEpochs, tracker.token())
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
		s.client2.log.Infof("Client 2 waiting %v for order %s to be partially matched", maxMatchDuration, tracker.token())
		matched := notes.find(ctx, maxMatchDuration, func(n Notification) bool {
			orderNote, isOrderNote := n.(*OrderNote)
			isMatchedTopic := n.Topic() == TopicBuyMatchesMade || n.Topic() == TopicSellMatchesMade
			return isOrderNote && isMatchedTopic && orderNote.Order.ID.String() == orderID
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

	s.log.Info("Orders placed and monitored to desired states.")

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
		s.client2.log.Infof("Client 2 order %v in expected pre-recovery status %v.", oid, expectStatus)
	}
	c2dc.tradeMtx.RUnlock()

	// Check trade-locked amount before disconnecting.
	c2Balance, err = s.client2.core.AssetBalance(s.base.id) // client 2 is seller
	if err != nil {
		return fmt.Errorf("client 2 pre-disconnect balance error %w", err)
	}
	s.client2.log.Infof("Client 2 %s available balance before disconnecting is %v.", s.base.symbol, s.base.valFmt(c2Balance.Available))
	totalLockedByTrades := c2Balance.Locked - preTradeLockedBalance
	preDisconnectLockedBalance := c2Balance.Locked   // should reduce after funds are returned
	preDisconnectAvailableBal := c2Balance.Available // should increase after funds are returned

	// Disconnect the DEX and allow some time for DEX to update order statuses.
	s.client2.log.Info("Disconnecting client 2 from the DEX server.")
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

	s.client2.enableWallets()
	// Disconnect the wallets, they'll be reconnected when Login is called below.
	// Login->connectWallets will error for btc spv wallets if the wallet is not
	// first disconnected.
	s.client2.disconnectWallets()
	// Disable after disconnect so the background bond maintenance loop
	// doesn't reconnect the old wallets (reopening the Badger tx history
	// DB) before initialize() replaces them with new wallet objects.
	s.client2.disableWallets()

	// Allow some time for orders to be revoked due to inaction, and
	// for requests pending on the server to expire (usually bTimeout).
	bTimeout := time.Millisecond * time.Duration(c2dc.cfg.BroadcastTimeout)
	disconnectPeriod := 2 * bTimeout
	s.client2.log.Infof("Waiting %v before reconnecting client 2 to DEX.", disconnectPeriod)
	time.Sleep(disconnectPeriod)

	s.client2.log.Info("Reconnecting client 2 to DEX to trigger order status reconciliation.")

	// Use core.initialize to restore client 2 orders from db, and login
	// to trigger dex authentication.
	// TODO: cannot do this anymore with built-in wallets
	err = s.client2.core.initialize()
	if err != nil {
		return fmt.Errorf("client 2 initialize error: %w", err)
	}
	// Reset loggedIn so Login re-runs resolveActiveTrades, which loads
	// orders from the db into the new dexConnection created by initialize.
	s.client2.core.loginMtx.Lock()
	s.client2.core.loggedIn = false
	s.client2.core.loginMtx.Unlock()
	err = s.client2.core.Login(s.client2.appPass)
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
		s.client2.log.Infof("Client 2 order %v in expected post-recovery status %v.", oid, expectStatus)
	}
	c2dc.tradeMtx.RUnlock()

	// Wait for tick cycles to trigger inactive trade retirement and funds
	// unlocking. Revoked matches at MakerSwapCast may need multiple tick
	// cycles to fully process and return locked coins.
	twoBTimeout := 2 * time.Millisecond * time.Duration(c2dc.cfg.BroadcastTimeout)
	time.Sleep(twoBTimeout)

	c2Balance, err = s.client2.core.AssetBalance(s.base.id) // client 2 is seller
	if err != nil {
		return fmt.Errorf("client 2 post-reconnect balance error %w", err)
	}
	s.client2.log.Infof("Client 2 %s available balance after reconnecting to DEX is %v.", s.base.symbol, s.base.valFmt(c2Balance.Available))
	if c2Balance.Available != preDisconnectAvailableBal+totalLockedByTrades {
		return fmt.Errorf("client 2 locked funds not returned: locked before trading %v, locked after trading %v, "+
			"locked after reconnect %v", preTradeLockedBalance, preDisconnectLockedBalance, c2Balance.Locked)
	}
	if c2Balance.Locked != preDisconnectLockedBalance-totalLockedByTrades {
		return fmt.Errorf("client 2 locked funds not returned: locked before trading %v, locked after trading %v, "+
			"locked after reconnect %v", preTradeLockedBalance, preDisconnectLockedBalance, c2Balance.Locked)
	}
	s.client2.enableWallets()

	for _, c := range s.clients {
		if err := c.mineMedian(context.TODO(), s.quote.id); err != nil {
			return err
		}
		if err := c.mineMedian(context.TODO(), s.base.id); err != nil {
			return err
		}
	}

	s.waitALittleBit()

	return nil
}

// TestResendPendingRequests runs a simple trade test, simulates init/redeem
// request errors during trade negotiation and ensures that failed requests
// are retried and the trades complete successfully.
func testResendPendingRequests(s *simulationTest) error {
	var qty, rate uint64 = 1 * s.lotSize, s.clampRate(250 * s.rateStep)
	s.client1.isSeller, s.client2.isSeller = true, false

	anErr := errors.New("intentional error from test")
	var bits uint8
	// Fail every first try of init and redeem. Second try will be
	// passed on to the real comms.
	preFilter1 := func(route string) error {
		if route == msgjson.InitRoute {
			if bits&0b1 == 0 {
				bits |= 0b1
				return anErr
			}
		}
		if route == msgjson.RedeemRoute {
			if bits&0b10 == 0 {
				bits |= 0b10
				return anErr
			}
		}
		return nil
	}

	preFilter2 := func(route string) error {
		if route == msgjson.InitRoute {
			if bits&0b100 == 0 {
				bits |= 0b100
				return anErr
			}
		}
		if route == msgjson.RedeemRoute {
			if bits&0b1000 == 0 {
				bits |= 0b1000
				return anErr
			}
		}
		return nil
	}

	defer s.client1.filteredConn.withRequestFilter(preFilter1)()
	defer s.client2.filteredConn.withRequestFilter(preFilter2)()

	return s.simpleTradeTest(qty, rate, order.MatchConfirmed)
}

// testNoTakerAddr places orders that get matched, but the taker's match ack
// has its per-match swap address stripped. The server should reject the ack,
// and after bTimeout the match is revoked with the taker at fault
// (OutcomeNoAddrAsTaker). The maker should not be penalized.
func testNoTakerAddr(s *simulationTest) error {
	var qty, rate uint64 = 1 * s.lotSize, s.clampRate(100 * s.rateStep)
	s.client1.isSeller, s.client2.isSeller = true, false

	// Determine which client will be the taker. The first order placed
	// gets booked and becomes the maker; the second becomes the taker.
	taker := s.client2
	if !s.client1IsMaker {
		taker = s.client1
	}

	// Install a send filter on the taker that strips the Address field
	// from match acknowledgement responses. The server validates the
	// address and will reject the ack when it's empty.
	stripAddr := func(msg *msgjson.Message) *msgjson.Message {
		if msg.Type != msgjson.Response {
			return msg
		}
		resp, err := msg.Response()
		if err != nil || resp.Result == nil {
			return msg
		}
		var acks []*msgjson.Acknowledgement
		if err := json.Unmarshal(resp.Result, &acks); err != nil {
			return msg // not an ack response
		}
		if len(acks) == 0 || acks[0].Address == "" {
			return msg // not a match ack or already empty
		}
		for _, ack := range acks {
			ack.Address = ""
		}
		result, err := json.Marshal(acks)
		if err != nil {
			return msg
		}
		newResp := &msgjson.ResponsePayload{
			Result: result,
			Error:  resp.Error,
		}
		encResp, err := json.Marshal(newResp)
		if err != nil {
			return msg
		}
		msg.Payload = encResp
		return msg
	}
	defer taker.filteredConn.withSendFilter(stripAddr)()

	c1OrderID, c2OrderID, err := s.placeTestOrders(qty, rate)
	if err != nil {
		return err
	}

	// Disable wallets after orders are placed so the match doesn't
	// progress past NewlyMatched. The match should be revoked due to
	// the missing taker address before the maker would need to swap.
	for _, client := range s.clients {
		client.disableWallets()
		defer client.enableWallets()
	}

	monitorTrades, ctx := errgroup.WithContext(context.Background())
	monitorTrades.Go(func() error {
		return s.monitorOrderMatchingAndTradeNeg(ctx, s.client1, c1OrderID, order.NewlyMatched)
	})
	monitorTrades.Go(func() error {
		return s.monitorOrderMatchingAndTradeNeg(ctx, s.client2, c2OrderID, order.NewlyMatched)
	})
	if err = monitorTrades.Wait(); err != nil {
		return err
	}

	// The match should have been revoked at NewlyMatched because the
	// server rejected the taker's empty address. We don't assert on
	// CounterPartyAddr because the connect response recovery path
	// (compareServerMatches) may populate it from the maker's address
	// that the server already has, even though the counterparty_address
	// notification was never sent.

	s.waitALittleBit()

	if accountBIPs[s.base.id] || accountBIPs[s.quote.id] {
		s.log.Info("Skipping balance assertion (account-based asset in market).")
	} else {
		for _, client := range s.clients {
			if err = s.assertBalanceChanges(client, false); err != nil {
				return err
			}
		}
	}

	// Check refunds.
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

	s.log.Info("Trades revoked as expected due to missing taker per-match address.")
	return nil
}

// testNoMakerAddr is the mirror of testNoTakerAddr. The maker's match ack has
// its per-match swap address stripped. The server rejects the ack, the match
// times out at NewlyMatched, and the maker is faulted (OutcomeNoSwapAsMaker).
func testNoMakerAddr(s *simulationTest) error {
	var qty, rate uint64 = 1 * s.lotSize, s.clampRate(100 * s.rateStep)
	s.client1.isSeller, s.client2.isSeller = true, false

	// Determine which client will be the maker.
	maker := s.client1
	if !s.client1IsMaker {
		maker = s.client2
	}

	// Install a send filter on the maker that strips the Address field
	// from match acknowledgement responses.
	stripAddr := func(msg *msgjson.Message) *msgjson.Message {
		if msg.Type != msgjson.Response {
			return msg
		}
		resp, err := msg.Response()
		if err != nil || resp.Result == nil {
			return msg
		}
		var acks []*msgjson.Acknowledgement
		if err := json.Unmarshal(resp.Result, &acks); err != nil {
			return msg
		}
		if len(acks) == 0 || acks[0].Address == "" {
			return msg
		}
		for _, ack := range acks {
			ack.Address = ""
		}
		result, err := json.Marshal(acks)
		if err != nil {
			return msg
		}
		newResp := &msgjson.ResponsePayload{
			Result: result,
			Error:  resp.Error,
		}
		encResp, err := json.Marshal(newResp)
		if err != nil {
			return msg
		}
		msg.Payload = encResp
		return msg
	}
	defer maker.filteredConn.withSendFilter(stripAddr)()

	c1OrderID, c2OrderID, err := s.placeTestOrders(qty, rate)
	if err != nil {
		return err
	}

	// Disable wallets after orders are placed so the match doesn't
	// progress past NewlyMatched.
	for _, client := range s.clients {
		client.disableWallets()
		defer client.enableWallets()
	}

	monitorTrades, ctx := errgroup.WithContext(context.Background())
	monitorTrades.Go(func() error {
		return s.monitorOrderMatchingAndTradeNeg(ctx, s.client1, c1OrderID, order.NewlyMatched)
	})
	monitorTrades.Go(func() error {
		return s.monitorOrderMatchingAndTradeNeg(ctx, s.client2, c2OrderID, order.NewlyMatched)
	})
	if err = monitorTrades.Wait(); err != nil {
		return err
	}

	// The match should have been revoked because the server rejected the
	// maker's empty address. We don't assert on CounterPartyAddr because
	// the connect response recovery path may populate it even though the
	// counterparty_address notification was never sent.

	s.waitALittleBit()

	if accountBIPs[s.base.id] || accountBIPs[s.quote.id] {
		s.log.Info("Skipping balance assertion (account-based asset in market).")
	} else {
		for _, client := range s.clients {
			if err = s.assertBalanceChanges(client, false); err != nil {
				return err
			}
		}
	}

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

	s.log.Info("Trades revoked as expected due to missing maker per-match address.")
	return nil
}

// testNoTakerRedeem runs a trade through to MakerRedeemed, then prevents the
// taker from broadcasting their redeem. The match should time out with the
// taker at fault (OutcomeNoRedeemAsTaker, score -1). The maker has already
// redeemed successfully, so only the taker is penalized.
func testNoTakerRedeem(s *simulationTest) error {
	var qty, rate uint64 = 1 * s.lotSize, s.clampRate(250 * s.rateStep)
	s.client1.isSeller, s.client2.isSeller = true, false

	// Block the taker's redeem request. We don't know which client will
	// be taker ahead of time, so install filters on both that block
	// redeem only for the taker side.
	anErr := errors.New("intentional error from test")
	var takerRedeemBlocked uint32
	makeFilter := func(client *simulationClient) func(route string) error {
		return func(route string) error {
			if route == msgjson.RedeemRoute {
				// The first client to attempt a redeem is the maker.
				// Let it through. The second (taker) gets blocked.
				if atomic.AddUint32(&takerRedeemBlocked, 1) > 1 {
					return anErr
				}
			}
			return nil
		}
	}

	defer s.client1.filteredConn.withRequestFilter(makeFilter(s.client1))()
	defer s.client2.filteredConn.withRequestFilter(makeFilter(s.client2))()

	// Use MatchComplete because the taker still redeems on-chain (it
	// extracts the secret from the maker's redeem tx). The filter only
	// blocks the redeem notification to the server, not the on-chain
	// broadcast.
	return s.simpleTradeTest(qty, rate, order.MatchComplete)
}

// testMissedCounterPartyAddr verifies that a client who misses the initial
// counterparty_address notification still receives the address on reconnect.
// One client disconnects immediately after matching (before the address
// notification arrives), reconnects, and the trade completes.
func testMissedCounterPartyAddr(s *simulationTest) error {
	var qty, rate uint64 = 1 * s.lotSize, s.clampRate(150 * s.rateStep)
	s.client1.isSeller, s.client2.isSeller = true, false

	// The taker will disconnect right after acking the match. Install a
	// notification filter that drops the counterparty_address message and
	// triggers a disconnect.
	taker := s.client2
	if !s.client1IsMaker {
		taker = s.client1
	}

	// Strategy: after orders are placed and matched, immediately
	// disconnect the taker and reconnect. The server's UserConnected
	// will re-send counterparty_address notifications for active matches.

	c1OrderID, c2OrderID, err := s.placeTestOrders(qty, rate)
	if err != nil {
		return err
	}

	// Wait for match.
	takerOrderID := c2OrderID
	if !s.client1IsMaker {
		takerOrderID = c1OrderID
	}
	tracker, err := taker.findOrder(takerOrderID)
	if err != nil {
		return err
	}
	maxMatchDuration := 2 * time.Duration(tracker.epochLen()) * time.Millisecond
	matched := taker.notes.find(s.ctx, maxMatchDuration, func(n Notification) bool {
		orderNote, isOrderNote := n.(*OrderNote)
		isMatchedTopic := n.Topic() == TopicBuyMatchesMade || n.Topic() == TopicSellMatchesMade
		return isOrderNote && isMatchedTopic && orderNote.Order.ID.String() == takerOrderID
	})
	if !matched {
		return fmt.Errorf("taker order %s not matched after %s", tracker.token(), maxMatchDuration)
	}
	taker.log.Info("Taker matched. Clearing CounterPartyAddr to simulate missed notification.")

	// Clear CounterPartyAddr to simulate having missed the
	// counterparty_address notification. We can't reliably race the
	// notification because it arrives in the same message batch as the
	// match.
	tracker.mtx.Lock()
	for _, match := range tracker.matches {
		match.MetaData.CounterPartyAddr = ""
	}
	tracker.mtx.Unlock()

	// Disconnect the taker.
	takerDC := taker.dc()
	takerDC.connMaster.Disconnect()
	disconnectTimeout := 10 * sleepFactor * time.Second
	disconnected := taker.notes.find(context.Background(), disconnectTimeout, func(n Notification) bool {
		connNote, ok := n.(*ConnEventNote)
		return ok && connNote.Host == dexHost && connNote.ConnectionStatus != comms.Connected
	})
	if !disconnected {
		return fmt.Errorf("taker not disconnected after %v", disconnectTimeout)
	}
	taker.log.Info("Taker disconnected with cleared CounterPartyAddr.")

	// Brief pause, then reconnect.
	time.Sleep(2 * time.Second * sleepFactor)
	taker.disconnectWallets()
	err = taker.core.initialize()
	if err != nil {
		return fmt.Errorf("taker re-initialize error: %w", err)
	}
	// Reset loggedIn so Login re-runs resolveActiveTrades, which loads
	// orders from the db into the new dexConnection created by initialize.
	taker.core.loginMtx.Lock()
	taker.core.loggedIn = false
	taker.core.loginMtx.Unlock()
	err = taker.core.Login(taker.appPass)
	if err != nil {
		return fmt.Errorf("taker login error: %w", err)
	}
	taker.replaceConns()
	taker.log.Info("Taker reconnected. Expecting counterparty_address re-delivery.")

	// Now let both clients run the trade to completion.
	c1Tracker, err := s.client1.findOrder(c1OrderID)
	if err != nil {
		return fmt.Errorf("client 1 find order error after reconnect: %w", err)
	}
	takerTracker, err := taker.findOrder(takerOrderID)
	if err != nil {
		return fmt.Errorf("taker find order error after reconnect: %w", err)
	}
	var monitorTrades errgroup.Group
	monitorTrades.Go(func() error {
		return s.monitorTrackedTrade(s.client1, c1Tracker, order.MatchConfirmed)
	})
	monitorTrades.Go(func() error {
		return s.monitorTrackedTrade(taker, takerTracker, order.MatchConfirmed)
	})
	if err = monitorTrades.Wait(); err != nil {
		return err
	}

	// Verify CounterPartyAddr was populated after reconnection. Use the
	// tracker reference from before the trade completed, since the order
	// may have been archived and removed from the trades map.
	takerTracker.mtx.RLock()
	for _, match := range takerTracker.matches {
		if match.MetaData.CounterPartyAddr == "" {
			takerTracker.mtx.RUnlock()
			return fmt.Errorf("taker match %s still has empty CounterPartyAddr after reconnect and trade completion",
				token(match.MatchID[:]))
		}
		taker.log.Infof("Match %s CounterPartyAddr restored after reconnect: %s.",
			token(match.MatchID[:]), match.MetaData.CounterPartyAddr)
	}
	takerTracker.mtx.RUnlock()

	s.waitALittleBit()

	// Set expected balance diffs for the completed trade. monitorTrackedTrade
	// does not update expectBalanceDiffs (that's done by
	// monitorOrderMatchingAndTradeNeg), so we set them manually.
	// client1 is seller, client2 is buyer.
	baseAmt := int64(qty)
	quoteAmt := int64(calc.BaseToQuote(rate, qty))
	s.client1.expectBalanceDiffs = map[uint32]int64{
		s.base.id:  -baseAmt,
		s.quote.id: quoteAmt,
	}
	s.client2.expectBalanceDiffs = map[uint32]int64{
		s.base.id:  baseAmt,
		s.quote.id: -quoteAmt,
	}

	if accountBIPs[s.base.id] || accountBIPs[s.quote.id] {
		s.log.Info("Skipping balance assertion (account-based asset in market).")
	} else {
		for _, client := range s.clients {
			if err = s.assertBalanceChanges(client, false); err != nil {
				return err
			}
		}
	}

	s.log.Info("Trade completed successfully after counterparty_address re-delivery on reconnect.")
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
			defer client.enableWallets()
		}

		s.log.Info("Disabled wallets to prevent order status from moving past order.NewlyMatched")
	}

	if finalStatus == order.MakerSwapCast {
		// Kill the wallets to prevent Taker from sending swap as soon
		// as the orders are matched.
		name := s.client1.name
		if s.client1IsMaker {
			name = s.client2.name
			s.client2.disableWallets()
			defer s.client2.enableWallets()
		} else {
			s.client1.disableWallets()
			defer s.client1.enableWallets()
		}
		s.log.Infof("Disabled client %s wallets to prevent order status from moving past order.MakerSwapCast.", name)
	}

	// NOTE: Client 1 is always maker the first time this test is run and
	// taker the second when running the same test twice.
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
	s.waitALittleBit()

	for _, client := range s.clients {
		// Skip balance assertions for account-based (EVM) assets. EVM
		// balance accounting is unreliable in sequential test runs for
		// several reasons: dynamic fees are not populated in FeesPaid
		// until a later trade tick processes the mined tx receipt, gas
		// costs and pre-funded reserves are not reflected in the
		// expected diff, and pending transactions from previous tests
		// (refunds, redeems) can confirm during the current test when
		// blocks are mined, contaminating the balance diff. The swap
		// amounts are enforced by the smart contract (reverts on wrong
		// value), so incorrect amounts would surface as swap failures.
		// UTXO balance assertions remain useful and reliable.
		if accountBIPs[s.base.id] || accountBIPs[s.quote.id] {
			s.log.Infof("Skipping balance assertion for client %s (account-based asset in market).",
				client.name)
			// Still update balances so the refund assertion (if any)
			// compares against the post-swap baseline, not pre-trade.
			if err = s.updateBalances(client); err != nil {
				return err
			}
			continue
		}
		if err = s.assertBalanceChanges(client, false); err != nil {
			return err
		}
	}

	// Check if any refunds are necessary and wait to ensure the refunds
	// are completed.
	if finalStatus < order.MatchConfirmed {
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

	var (
		c1OrderID, c2OrderID string
		err                  error
	)
	placeOrderC1 := func() error {
		c1OrderID, err = s.placeOrder(s.client1, qty, rate, false)
		if err != nil {
			return fmt.Errorf("client1 place %s order error: %v", sellString(s.client1.isSeller), err)
		}
		return nil
	}

	placeOrderC2 := func() error {
		c2OrderID, err = s.placeOrder(s.client2, qty, rate, false)
		if err != nil {
			return fmt.Errorf("client2 place %s order error: %v", sellString(s.client2.isSeller), err)
		}
		return nil
	}

	// The client to have their order booked first becomes the maker of a
	// trade. Here the second time a trade is run for the same
	// simulationTest make and taker will be swapped allowing testing for
	// both sides at different stages of failure to act and their resolution.
	var (
		client  *simulationClient
		orderID string
	)
	if s.client1IsMaker {
		if err = placeOrderC1(); err != nil {
			return "", "", err
		}
		orderID = c1OrderID
		client = s.client1
	} else {
		if err = placeOrderC2(); err != nil {
			return "", "", err
		}
		orderID = c2OrderID
		client = s.client2
	}

	tracker, err := client.findOrder(orderID)
	if err != nil {
		return "", "", err
	}
	// Wait the epoch duration for this order to get booked.
	epochDur := time.Duration(tracker.epochLen()) * time.Millisecond
	time.Sleep(epochDur)

	if s.client1IsMaker {
		if err = placeOrderC2(); err != nil {
			return "", "", err
		}
	} else {
		if err = placeOrderC1(); err != nil {
			return "", "", err
		}
	}

	return c1OrderID, c2OrderID, nil
}

func (s *simulationTest) monitorOrderMatchingAndTradeNeg(ctx context.Context, client *simulationClient, orderID string, finalStatus order.MatchStatus) error {
	errs := newErrorSet("[client %s] ", client.name)

	client.log.Infof("Client %s starting to monitor order %s.", client.name, orderID)

	tracker, err := client.findOrder(orderID)
	if err != nil {
		return errs.addErr(err)
	}

	// Wait up to 2 times the epoch duration for this order to get matched.
	maxMatchDuration := 2 * time.Duration(tracker.epochLen()) * time.Millisecond
	client.log.Infof("Client %s Waiting up to %v for matches on order %s.", client.name, maxMatchDuration, tracker.token())
	matched := client.notes.find(ctx, maxMatchDuration, func(n Notification) bool {
		orderNote, isOrderNote := n.(*OrderNote)
		isMatchedTopic := n.Topic() == TopicBuyMatchesMade || n.Topic() == TopicSellMatchesMade
		return isOrderNote && isMatchedTopic && orderNote.Order.ID.String() == orderID
	})
	if ctx.Err() != nil { // context canceled
		return nil
	}
	if !matched {
		return errs.add("order %s not matched after %s", tracker.token(), maxMatchDuration)
	}

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
		client.log.Infof("Updated %s balance diff with %s.", a.symbol, a.valFmt(amt))
		client.expectBalanceDiffs[a.id] += amt
	}

	tracker.mtx.RLock()
	client.log.Infof("Client %s %d match(es) received for order %s.", client.name, len(tracker.matches), tracker.token())
	swapAddrs := make(map[string]order.MatchID, len(tracker.matches))
	for _, match := range tracker.matches {
		client.log.Infof("Client %s is %s on match %s, amount %s %s.", client.name, match.Side.String(),
			token(match.MatchID.Bytes()), s.base.valFmt(match.Quantity), s.base.symbol)

		// Verify per-match swap address was generated.
		if match.MetaData.SwapAddr == "" {
			tracker.mtx.RUnlock()
			return errs.add("match %s has no per-match SwapAddr", token(match.MatchID.Bytes()))
		}
		if prev, exists := swapAddrs[match.MetaData.SwapAddr]; exists {
			tracker.mtx.RUnlock()
			return errs.add("match %s reuses SwapAddr %s from match %s",
				token(match.MatchID.Bytes()), match.MetaData.SwapAddr, token(prev.Bytes()))
		}
		swapAddrs[match.MetaData.SwapAddr] = match.MatchID
		client.log.Infof("Match %s per-match SwapAddr: %s.", token(match.MatchID.Bytes()), match.MetaData.SwapAddr)

		if match.Side == order.Taker {
			if finalStatus >= order.TakerSwapCast {
				recordBalanceChanges(true, match.Quantity, match.Rate)
			}
			if finalStatus >= order.MatchComplete {
				recordBalanceChanges(false, match.Quantity, match.Rate)
			}
		} else { // maker
			if finalStatus >= order.MakerSwapCast {
				recordBalanceChanges(true, match.Quantity, match.Rate)
			}
			if finalStatus >= order.MakerRedeemed {
				recordBalanceChanges(false, match.Quantity, match.Rate)
			}
		}
	}
	tracker.mtx.RUnlock()

	return s.monitorTrackedTrade(client, tracker, finalStatus)
}

func (s *simulationTest) monitorTrackedTrade(client *simulationClient, tracker *trackedTrade, finalStatus order.MatchStatus) error {
	// run a repeated check for match status changes to mine blocks as necessary.
	maxTradeDuration := 4 * time.Minute
	var waitedForOtherSideMakerInit, waitedForOtherSideTakerInit bool

	tryUntil(s.ctx, maxTradeDuration, func() bool {

		var completedTrades int
		mineAssets := make(map[uint32]uint32)
		var waitForOtherSideMakerInit, waitForOtherSideTakerInit bool
		var thisSide uint32
		// Don't spam.
		time.Sleep(time.Second * 2 * sleepFactor)
		tracker.mtx.Lock()
		for _, match := range tracker.matches {
			side, status := match.Side, match.Status
			client.psMTX.Lock()
			lastStatus := client.processedStatus[match.MatchID]
			client.psMTX.Unlock()
			if status >= finalStatus && lastStatus >= finalStatus {
				match.swapErr = fmt.Errorf("take no further action")
				completedTrades++
				continue
			}
			if status != lastStatus {
				client.log.Infof("Match %s: NOW =====> %s.", match.MatchID, status)
				client.psMTX.Lock()
				client.processedStatus[match.MatchID] = status
				client.psMTX.Unlock()
			}

			logIt := func(swapOrRedeem string, assetID, nBlocks uint32) {
				var actor order.MatchSide
				if swapOrRedeem == "redeem" {
					actor = side // this client
				} else if side == order.Maker {
					actor = order.Taker // counter-party
				} else {
					actor = order.Maker
				}
				client.log.Infof("Mining %d %s blocks for %s's %s, match %s.", nBlocks, unbip(assetID),
					actor, swapOrRedeem, token(match.MatchID.Bytes()))
			}

			if (side == order.Maker && status <= order.MakerSwapCast && finalStatus >= order.MakerSwapCast) ||
				(side == order.Taker && status <= order.TakerSwapCast && finalStatus >= order.TakerSwapCast) {
				// If the other side is geth, we need to give it
				// time to confirm the swap in order to populate
				// swap fees.
				if !waitedForOtherSideMakerInit && accountBIPs[tracker.wallets.toWallet.AssetID] {
					thisSide = tracker.wallets.fromWallet.AssetID
					waitForOtherSideMakerInit = true
				}
				if !waitedForOtherSideTakerInit && status > order.MakerSwapCast &&
					accountBIPs[tracker.wallets.toWallet.AssetID] {
					thisSide = tracker.wallets.fromWallet.AssetID
					waitForOtherSideTakerInit = true
				}
				// Progress from asset.
				nBlocks := tracker.metaData.FromSwapConf
				if accountBIPs[tracker.wallets.fromWallet.AssetID] {
					nBlocks = 8
				}
				assetID := tracker.wallets.fromWallet.AssetID
				mineAssets[assetID] = nBlocks
				logIt("swap", assetID, nBlocks)
			}
			if (side == order.Maker && status > order.TakerSwapCast && finalStatus > order.TakerSwapCast) ||
				(side == order.Taker && status > order.MakerRedeemed && finalStatus > order.MakerRedeemed) {
				if !waitedForOtherSideMakerInit && accountBIPs[tracker.wallets.fromWallet.AssetID] {
					thisSide = tracker.wallets.toWallet.AssetID
					waitForOtherSideMakerInit = true
				}
				if !waitedForOtherSideTakerInit && status > order.MakerSwapCast &&
					accountBIPs[tracker.wallets.fromWallet.AssetID] {
					thisSide = tracker.wallets.toWallet.AssetID
					waitForOtherSideTakerInit = true
				}
				// Progress to asset.
				nBlocks := tracker.metaData.ToSwapConf
				if accountBIPs[tracker.wallets.toWallet.AssetID] {
					nBlocks = 8
				}
				assetID := tracker.wallets.toWallet.AssetID
				mineAssets[assetID] = nBlocks
				logIt("redeem", assetID, nBlocks)
			}
		}

		finish := completedTrades == len(tracker.matches)
		// Do not hold the lock while mining as this hinders trades.
		tracker.mtx.Unlock()

		// Geth light client takes time for the best block to be updated
		// after mining. Sleeping to get confs registered for the init
		// before the match can be confirmed if the utxo side completes
		// too fast.
		if waitForOtherSideMakerInit || waitForOtherSideTakerInit {
			client.log.Infof("Not mining asset %d so geth can find init confirms.", thisSide)
			delete(mineAssets, thisSide)
		}
		mine := func(assetID, nBlocks uint32) {
			err := newHarnessCtrl(assetID).mineBlocks(s.ctx, nBlocks)
			if err != nil {
				client.log.Infof("%s mine error %v.", unbip(assetID), err) // return err???
			}
		}
		for assetID, swapConf := range mineAssets {
			mine(assetID, swapConf)
		}
		if waitForOtherSideMakerInit || waitForOtherSideTakerInit {
			client.log.Info("Sleeping 8 seconds for geth to update other side's init best block.")
			time.Sleep(time.Second * 8)
			if waitForOtherSideMakerInit {
				waitedForOtherSideMakerInit = true
			}
			if waitForOtherSideTakerInit {
				waitedForOtherSideTakerInit = true
			}
		}
		return finish
	})
	if s.ctx.Err() != nil { // context canceled
		return nil
	}

	var incompleteTrades int
	tracker.mtx.RLock()
	for _, match := range tracker.matches {
		if match.Status < finalStatus {
			incompleteTrades++
			client.log.Infof("Incomplete trade: order %s, match %s, status %s, side %s.", tracker.token(),
				token(match.MatchID[:]), match.Status, match.Side)
		} else {
			client.log.Infof("Trade for order %s, match %s monitored successfully till %s, side %s.", tracker.token(),
				token(match.MatchID[:]), match.Status, match.Side)
		}
		// Any match that progressed to swapping must have received
		// the counterparty's per-match address.
		if match.Status >= order.MakerSwapCast && match.MetaData.CounterPartyAddr == "" {
			tracker.mtx.RUnlock()
			return fmt.Errorf("client %s match %s reached %s without receiving CounterPartyAddr",
				client.name, token(match.MatchID[:]), match.Status)
		}
		if match.MetaData.CounterPartyAddr != "" {
			client.log.Infof("Match %s CounterPartyAddr confirmed: %s.", token(match.MatchID[:]), match.MetaData.CounterPartyAddr)
		}
	}
	tracker.mtx.RUnlock()
	if incompleteTrades > 0 {
		return fmt.Errorf("client %s reported %d incomplete trades for order %s after %s",
			client.name, incompleteTrades, tracker.token(), maxTradeDuration)
	}

	return nil
}

// swaps cannot be refunded until the MedianTimePast is greater than
// the swap locktime. The MedianTimePast is calculated by taking the
// timestamps of the last 11 blocks and finding the median. Mining 6
// blocks on the chain a second from now will ensure that the
// MedianTimePast will be greater than the furthest swap locktime,
// thereby lifting the time lock on all these swaps.
func (client *simulationClient) mineMedian(ctx context.Context, assetID uint32) error {
	time.Sleep(sleepFactor * time.Second)
	if err := newHarnessCtrl(assetID).mineBlocks(ctx, 6); err != nil {
		return fmt.Errorf("client %s: error mining 6 %s blocks for swap refunds: %v",
			client.name, unbip(assetID), err)
	}
	client.log.Infof("Mined 6 blocks for assetID %d to expire swap locktimes.", assetID)
	return nil
}

func (s *simulationTest) checkAndWaitForRefunds(ctx context.Context, client *simulationClient, orderID string) error {
	// check if client has pending refunds
	client.log.Infof("Checking if refunds are necessary for client %s.", client.name)
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

	if tracker == nil {
		return nil
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
		refundAmts[tracker.wallets.fromWallet.AssetID] += int64(swapAmt)

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
		client.log.Infof("No refunds necessary for client %s.", client.name)
		return nil
	}

	client.log.Infof("Found refundable swaps worth %s %s and %s %s.", s.base.valFmt(refundAmts[s.base.id]),
		s.base.symbol, s.quote.valFmt(refundAmts[s.quote.id]), s.quote.symbol)

	// wait for refunds to be executed
	now := time.Now()
	if furthestLockTime.After(now) {
		wait := furthestLockTime.Sub(now)
		client.log.Infof("Waiting until the longest timelock expires at %v before checking "+
			"client %s wallet balances for expected refunds.", wait, client.name)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
	}

	if refundAmts[s.quote.id] > 0 {
		if err := client.mineMedian(ctx, s.quote.id); err != nil {
			return err
		}
	}
	if refundAmts[s.base.id] > 0 {
		if err := client.mineMedian(ctx, s.base.id); err != nil {
			return err
		}
	}

	// allow up to 30 seconds for core to get around to refunding the swaps
	var notRefundedSwaps int
	refundWaitTimeout := 60 * time.Second
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
	s.waitALittleBit()

	// For account-based (EVM) assets, skip the refund balance assertion.
	// The refund was already verified above (RefundCoin != nil). EVM
	// wallet balance accounting with pre-funded reserves makes the
	// simple before/after diff unreliable across the locked-to-available
	// transition that happens when a trade concludes.
	for assetID, amt := range refundAmts {
		if amt > 0 && accountBIPs[assetID] {
			client.log.Infof("Skipping refund balance assertion for %s (account-based asset).", unbip(assetID))
			client.log.Infof("Successfully refunded swaps worth %s %s and %s %s.", s.base.valFmt(refundAmts[s.base.id]),
				s.base.symbol, s.quote.valFmt(refundAmts[s.quote.id]), s.quote.symbol)
			return nil
		}
	}

	client.expectBalanceDiffs = refundAmts
	err = s.assertBalanceChanges(client, true)
	if err == nil {
		client.log.Infof("Successfully refunded swaps worth %s %s and %s %s.", s.base.valFmt(refundAmts[s.base.id]),
			s.base.symbol, s.quote.valFmt(refundAmts[s.quote.id]), s.quote.symbol)
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

type harnessCtrl struct {
	dir, fundCmd, fundStr string
	// perTxMine is true for EVM assets where geth dev mode mines a block
	// per transaction automatically. No explicit mining is needed.
	perTxMine bool
}

func (hc *harnessCtrl) run(ctx context.Context, cmd string, args ...string) error {
	command := exec.CommandContext(ctx, cmd, args...)
	command.Dir = hc.dir
	r, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("exec error: running %q from directory %q, err = %q, output = %q",
			command, command.Dir, err, string(r))
	}
	return nil
}

func (hc *harnessCtrl) mineBlocks(ctx context.Context, n uint32) error {
	if !hc.perTxMine {
		return hc.run(ctx, "./mine-alpha", fmt.Sprintf("%d", n))
	}
	// EVM dev mode only mines when there are transactions. Send a
	// self-transfer to force a new block. Only one per call since
	// the monitor loop retries every 2s.
	return hc.run(ctx, "./sendtoaddress",
		"946dfaB1AD7caCFeF77dE70ea68819a30acD4577", "0")
}

func (hc *harnessCtrl) fund(ctx context.Context, address string, amts []int) error {
	for _, amt := range amts {
		fs := fmt.Sprintf(hc.fundStr, address, amt)
		strs := strings.Split(fs, "_")
		err := hc.run(ctx, hc.fundCmd, strs...)
		if err != nil {
			return err
		}
	}
	return nil
}

func newHarnessCtrl(assetID uint32) *harnessCtrl {
	symbolParts := strings.Split(dex.BipIDSymbol(assetID), ".")
	baseChainSymbol := symbolParts[0]
	if len(symbolParts) == 2 {
		baseChainSymbol = symbolParts[1]
	}
	switch assetID {
	case dcr.BipID, btc.BipID, ltc.BipID, bch.BipID, doge.BipID, firo.BipID, zec.BipID, zcl.BipID, dgb.BipID, dash.BipID:
		return &harnessCtrl{
			dir:     filepath.Join(dextestDir, baseChainSymbol, "harness-ctl"),
			fundCmd: "./alpha",
			fundStr: "sendtoaddress_%s_%d",
		}
	case eth.BipID, polygon.BipID, base.BipID:
		// Sending with values of .1 eth.
		return &harnessCtrl{
			dir:       filepath.Join(dextestDir, baseChainSymbol, "harness-ctl"),
			fundCmd:   "./sendtoaddress",
			fundStr:   "%s_%d",
			perTxMine: true,
		}
	case ethUsdcID, polygonUsdcID, baseUsdcID:
		return &harnessCtrl{
			dir:       filepath.Join(dextestDir, baseChainSymbol, "harness-ctl"),
			fundCmd:   "./sendUSDC",
			fundStr:   "%s_%d",
			perTxMine: true,
		}
	case ethUsdtID, polygonUsdtID, baseUsdtID:
		return &harnessCtrl{
			dir:       filepath.Join(dextestDir, baseChainSymbol, "harness-ctl"),
			fundCmd:   "./sendUSDT",
			fundStr:   "%s_%d",
			perTxMine: true,
		}
	}
	panic(fmt.Sprintf("unknown asset %d for harness control", assetID))
}

type tWallet struct {
	pass       []byte
	config     map[string]string
	walletType string // type is a keyword
	fund       bool
	hc         *harnessCtrl
	parent     *WalletForm
}

var cloneTypes = map[uint32]string{
	0:   "bitcoindRPC",
	2:   "litecoindRPC",
	20:  "digibytedRPC",
	145: "bitcoindRPC", // yes, same as btc
	3:   "dogecoindRPC",
	136: "firodRPC",
	133: "zcashdRPC",
	147: "zclassicdRPC",
	5:   "dashdRPC",
}

// accountBIPs is a map of account based assets. Used in fee estimation.
var accountBIPs = map[uint32]bool{
	eth.BipID:     true,
	ethUsdcID:     true,
	polygon.BipID: true,
	polygonUsdcID: true,
}

func dcrWallet(wt SimWalletType, node string) (*tWallet, error) {
	switch wt {
	case WTSPVNative:
		return &tWallet{
			walletType: "SPV",
			fund:       true,
		}, nil
	case WTCoreClone:
	default:
		return nil, fmt.Errorf("invalid wallet type: %v", wt)
	}

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

func btcWallet(wt SimWalletType, node string) (*tWallet, error) {
	return btcCloneWallet(btc.BipID, node, wt)
}

func ltcWallet(wt SimWalletType, node string) (*tWallet, error) {
	return btcCloneWallet(ltc.BipID, node, wt)
}

func bchWallet(wt SimWalletType, node string) (*tWallet, error) {
	return btcCloneWallet(bch.BipID, node, wt)
}

func firoWallet(wt SimWalletType, node string) (*tWallet, error) {
	return btcCloneWallet(firo.BipID, node, wt)
}

func ethWallet() (*tWallet, error) {
	return &tWallet{
		fund:       true,
		walletType: "rpc",
		config:     map[string]string{"providers": ethAlphaWSAddr},
	}, nil
}

func polygonWallet() (*tWallet, error) {
	return &tWallet{
		fund:       true,
		walletType: "rpc",
		config:     map[string]string{"providers": polygonAlphaWSAddr},
	}, nil
}

func usdcWallet() (*tWallet, error) {
	return &tWallet{
		fund:       true,
		walletType: "token",
		parent: &WalletForm{
			Type:    "rpc",
			AssetID: eth.BipID,
			Config:  map[string]string{"providers": ethAlphaWSAddr},
		},
	}, nil
}

func polyUsdcWallet() (*tWallet, error) {
	return &tWallet{
		fund:       true,
		walletType: "token",
		parent: &WalletForm{
			Type:    "rpc",
			AssetID: polygon.BipID,
			Config:  map[string]string{"providers": polygonAlphaWSAddr},
		},
	}, nil
}

func polyUsdtWallet() (*tWallet, error) {
	return &tWallet{
		fund:       true,
		walletType: "token",
		parent: &WalletForm{
			Type:    "rpc",
			AssetID: polygon.BipID,
			Config:  map[string]string{"providers": polygonAlphaWSAddr},
		},
	}, nil
}

func baseWallet() (*tWallet, error) {
	return &tWallet{
		fund:       true,
		walletType: "rpc",
		config:     map[string]string{"providers": baseAlphaWSAddr},
	}, nil
}

func baseTokenWallet() (*tWallet, error) {
	return &tWallet{
		fund:       true,
		walletType: "token",
		parent: &WalletForm{
			Type:    "rpc",
			AssetID: base.BipID,
			Config:  map[string]string{"providers": baseAlphaWSAddr},
		},
	}, nil
}

func btcCloneWallet(assetID uint32, node string, wt SimWalletType) (*tWallet, error) {
	switch wt {
	case WTSPVNative:
		return &tWallet{
			walletType: "SPV",
			fund:       true,
		}, nil
	case WTElectrum:
		// dex/testing/btc/electrum.sh
		cfg, err := config.Parse(filepath.Join(dextestDir, "electrum", dex.BipIDSymbol(assetID), "client-config.ini"))
		if err != nil {
			return nil, err
		}
		return &tWallet{
			walletType: "electrumRPC",
			pass:       []byte("abc"),
			config:     cfg,
			fund:       true,
		}, nil
	case WTCoreClone:
	default:
		return nil, fmt.Errorf("invalid wallet type: %v", wt)
	}

	rpcWalletType, ok := cloneTypes[assetID]
	if !ok {
		return nil, fmt.Errorf("invalid wallet type %v for asset %v", wt, assetID)
	}

	parentNode := node
	pass := []byte("abc")
	if node == "gamma" || node == "delta" || assetID == zec.BipID || assetID == zcl.BipID {
		if assetID != dash.BipID {
			pass = nil
		}
	}

	switch assetID {
	case doge.BipID, zec.BipID, zcl.BipID, firo.BipID:
	// dogecoind, zcashd and firod don't support > 1 wallet, so gamma and delta
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

	// doge fees are slightly higher than others. Leaving this as 0 will
	// apply bitcoin limits.
	switch assetID {
	case doge.BipID:
		cfg["fallbackfee"] = fmt.Sprintf("%f", dexdoge.DefaultFee*1000/1e8)
		cfg["feeratelimit"] = fmt.Sprintf("%f", dexdoge.DefaultFeeRateLimit*1000/1e8)
	case dgb.BipID:
		cfg["fallbackfee"] = fmt.Sprintf("%f", dexdgb.DefaultFee*1000/1e8)
		cfg["feeratelimit"] = fmt.Sprintf("%f", dexdgb.DefaultFeeRateLimit*1000/1e8)
	}

	return &tWallet{
		walletType: rpcWalletType,
		pass:       pass,
		config:     cfg,
	}, nil
}

func dogeWallet(node string) (*tWallet, error) {
	return btcCloneWallet(doge.BipID, node, WTCoreClone)
}

func dashWallet(node string) (*tWallet, error) {
	return btcCloneWallet(dash.BipID, node, WTCoreClone)
}

func dgbWallet(node string) (*tWallet, error) {
	return btcCloneWallet(dgb.BipID, node, WTCoreClone)
}

func zecWallet(node string) (*tWallet, error) {
	if node == "alpha" {
		return nil, errors.New("cannot use alpha wallet on Zcash")
	}
	cfg, err := config.Parse(filepath.Join(dextestDir, "zec", node, node+".conf"))
	if err != nil {
		return nil, err
	}
	return &tWallet{
		walletType: "zcashdRPC",
		config:     cfg,
	}, nil
}

func zclWallet(node string) (*tWallet, error) {
	return btcCloneWallet(zcl.BipID, node, WTCoreClone)
}

func (s *simulationTest) newClient(name string, cl *SimClient) (*simulationClient, error) {
	wallets := make(map[uint32]*tWallet, 2)
	addWallet := func(assetID uint32, wt SimWalletType, node string) error {
		var tw *tWallet
		var err error
		switch assetID {
		case dcr.BipID:
			tw, err = dcrWallet(wt, node)
		case btc.BipID:
			tw, err = btcWallet(wt, node)
		case eth.BipID:
			tw, err = ethWallet()
		case ethUsdcID, ethUsdtID:
			tw, err = usdcWallet()
		case polygon.BipID:
			tw, err = polygonWallet()
		case polygonUsdcID:
			tw, err = polyUsdcWallet()
		case polygonUsdtID:
			tw, err = polyUsdtWallet()
		case base.BipID:
			tw, err = baseWallet()
		case baseUsdcID, baseUsdtID:
			tw, err = baseTokenWallet()
		case ltc.BipID:
			tw, err = ltcWallet(wt, node)
		case bch.BipID:
			tw, err = bchWallet(wt, node)
		case doge.BipID:
			tw, err = dogeWallet(node)
		case dgb.BipID:
			tw, err = dgbWallet(node)
		case dash.BipID:
			tw, err = dashWallet(node)
		case firo.BipID:
			tw, err = firoWallet(wt, node)
		case zec.BipID:
			tw, err = zecWallet(node)
		case zcl.BipID:
			tw, err = zclWallet(node)
		default:
			return fmt.Errorf("no method to create wallet for asset %d", assetID)
		}
		if err != nil {
			return err
		}
		wallets[assetID] = tw
		return nil
	}
	if err := addWallet(s.base.id, cl.BaseWalletType, cl.BaseNode); err != nil {
		return nil, err
	}
	if err := addWallet(s.quote.id, cl.QuoteWalletType, cl.QuoteNode); err != nil {
		return nil, err
	}
	return &simulationClient{
		SimClient:       cl,
		name:            name,
		log:             s.log.SubLogger(name),
		appPass:         []byte(fmt.Sprintf("client-%s", name)),
		wallets:         wallets,
		processedStatus: make(map[order.MatchID]order.MatchStatus),
	}, nil
}

type simulationClient struct {
	*SimClient
	name  string
	log   dex.Logger
	wg    sync.WaitGroup
	core  *Core
	notes *notificationReader

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
	filteredConn       *tConn
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

	client.notes = client.startNotificationReader(ctx)
	return nil
}

func (client *simulationClient) replaceConns() {
	// Put the real comms in a fake comms we can induce request failures
	// with.
	client.core.connMtx.Lock()
	client.filteredConn = &tConn{
		WsConn: client.core.conns[dexHost].WsConn,
	}
	client.core.conns[dexHost].WsConn = client.filteredConn
	client.core.connMtx.Unlock()
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

	bondAsset := dexConf.BondAssets[feeAssetSymbol]
	if bondAsset == nil {
		return fmt.Errorf("%s not supported for fees!", feeAssetSymbol)
	}
	// Post enough bonds for a high tier so penalty tests don't cause
	// account suspension when running --all.
	postBondRes, err := client.core.PostBond(&PostBondForm{
		Addr:     dexHost,
		AppPass:  client.appPass,
		Asset:    &s.regAsset,
		Bond:     bondAsset.Amt * 10,
		LockTime: uint64(time.Now().Add(time.Hour * 24 * 30 * 5).Unix()),
	})
	if err != nil {
		return err
	}
	client.log.Infof("Sent registration fee to DEX %s.", dexHost)

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

	client.log.Infof("Mined %d %s blocks for fee payment confirmation.", postBondRes.ReqConfirms, feeAssetSymbol)

	// Wait up to bTimeout+12 seconds for fee payment. notify_fee times out
	// after bTimeout+10 seconds.
	feeTimeout := time.Millisecond*time.Duration(client.dc().cfg.BroadcastTimeout) + 12*time.Second
	client.log.Infof("Waiting %v for bond confirmation notice.", feeTimeout)
	feePaid := client.notes.find(s.ctx, feeTimeout, func(n Notification) bool {
		return n.Type() == NoteTypeBondPost && n.Topic() == TopicBondConfirmed
	})
	close(done)
	if !feePaid {
		return fmt.Errorf("fee payment not confirmed after %s", feeTimeout)
	}

	client.log.Infof("Fee payment confirmed for client %s.", client.name)
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
		feed: client.core.NotificationFeed().C,
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

	client.log.Infof("Client %s placed order %sing %s %s at %f %s/%s (%s).", client.name, sellString(client.isSeller),
		s.base.valFmt(qty), s.base.symbol, r, s.quote.symbol, s.base.symbol, ord.ID[:8])
	return ord.ID.String(), nil
}

func (s *simulationTest) updateBalances(client *simulationClient) error {
	client.log.Infof("Updating balances for client %s.", client.name)
	client.balances = make(map[uint32]uint64, len(client.wallets))
	setBalance := func(a *assetConfig) error {
		if parent := client.wallets[a.id].parent; parent != nil {
			parentSymbol := dex.BipIDSymbol(parent.AssetID)
			parentBalance, err := client.core.AssetBalance(parent.AssetID)
			if err != nil {
				return fmt.Errorf("error getting parent %s balance: %w", parentSymbol, err)
			}
			s.log.Infof("Client %s parent %s balance: available %s, immature %s, locked %s.", client.name, parentSymbol,
				a.valFmt(parentBalance.Available), a.valFmt(parentBalance.Immature), a.valFmt(parentBalance.Locked))
		}

		balances, err := client.core.AssetBalance(a.id)
		if err != nil {
			return err
		}
		client.balances[a.id] = balances.Available + balances.Immature + balances.Locked
		client.log.Infof("Client %s %s balance: available %s, immature %s, locked %s.", client.name, a.symbol,
			a.valFmt(balances.Available), a.valFmt(balances.Immature), a.valFmt(balances.Locked))
		return nil
	}
	if err := setBalance(s.base); err != nil {
		return err
	}
	return setBalance(s.quote)
}

func (s *simulationTest) assertBalanceChanges(client *simulationClient, isRefund bool) error {
	if client.expectBalanceDiffs == nil {
		return errors.New("balance diff is nil")
	}

	ord, err := client.core.Order(client.lastOrder)
	if err != nil {
		return errors.New("last order not found")
	}

	var baseFees, quoteFees int64
	if fees := ord.FeesPaid; fees != nil && !isRefund {
		if ord.Sell {
			baseFees = int64(fees.Swap + fees.Funding)
			quoteFees = int64(fees.Redemption)
		} else {
			quoteFees = int64(fees.Swap + fees.Funding)
			baseFees = int64(fees.Redemption)
		}
	}

	// Account assets require a refund fee in addition to the swap amount.
	if isRefund {
		// NOTE: Gas price may be higher if the eth harness has
		// had a lot of use. The minimum is the gas tip cap.
		ethRefundFees := int64(dexeth.RefundGas(1 /*version*/)) * dexeth.MinGasTipCap

		msgTx := wire.NewMsgTx(0)
		prevOut := wire.NewOutPoint(&chainhash.Hash{}, 0)
		txIn := wire.NewTxIn(prevOut, []byte{}, nil)
		msgTx.AddTxIn(txIn)
		size := dexbtc.MsgTxVBytes(msgTx) //wut? btc only
		// tx fee is 1sat/vByte on simnet. utxoRefundFees is 293 sats.
		// TODO: even simnet rate on other assets like dgb and doge isn't 1...
		utxoRefundFees := int64(size + dexbtc.RefundSigScriptSize + dexbtc.P2PKHOutputSize)
		// TODO: segwit proper fee rate, but need to get segwit flag:
		// size := btc.calcTxSize(msgTx)
		// if btc.segwit {
		// 	witnessVBytes := uint64((dexbtc.RefundSigScriptSize + 2 + 3) / 4)
		// 	size += witnessVBytes + dexbtc.P2WPKHOutputSize
		// } else {
		// 	size += dexbtc.RefundSigScriptSize + dexbtc.P2PKHOutputSize
		// }
		if ord.Sell {
			if accountBIPs[s.base.id] {
				baseFees = ethRefundFees
			} else {
				baseFees = utxoRefundFees
			}
		} else {
			if accountBIPs[s.quote.id] {
				quoteFees = ethRefundFees
			} else {
				quoteFees = utxoRefundFees
			}
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
		// TODO: account for actual fee(s) or use a more realistic fee rate
		// estimate.
		expVal := expDiff
		if !a.isToken {
			expVal -= fees
		}

		minExpectedDiff, maxExpectedDiff := int64(float64(expVal)*0.95), int64(float64(expVal)*1.05)

		// diffs can be negative
		if minExpectedDiff > maxExpectedDiff {
			minExpectedDiff, maxExpectedDiff = maxExpectedDiff, minExpectedDiff
		}

		balanceDiff := int64(client.balances[a.id]) - int64(prevBalances[a.id])
		if balanceDiff < minExpectedDiff || balanceDiff > maxExpectedDiff {
			return fmt.Errorf("%s balance change not in expected range %s - %s, got %s", a.symbol,
				a.valFmt(minExpectedDiff), a.valFmt(maxExpectedDiff), a.valFmt(balanceDiff))
		}
		client.log.Infof("%s balance change %s is in expected range of %s - %s.", a.symbol,
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
	tracker, _ := client.dc().findOrder(oid)
	return tracker, nil
}

func (client *simulationClient) disableWallets() {
	client.log.Infof("Disabling wallets for client %s.", client.name)
	client.core.walletMtx.Lock()
	for _, wallet := range client.core.wallets {
		wallet.setDisabled(true)
	}
	client.core.walletMtx.Unlock()
}

func (client *simulationClient) enableWallets() {
	client.log.Infof("Enabling wallets for client %s.", client.name)
	client.core.walletMtx.Lock()
	for _, wallet := range client.core.wallets {
		wallet.setDisabled(false)
	}
	client.core.walletMtx.Unlock()
}

func (client *simulationClient) disconnectWallets() {
	client.log.Infof("Disconnecting wallets for client %s.", client.name)
	client.core.walletMtx.Lock()
	for _, wallet := range client.core.wallets {
		wallet.Disconnect()
	}
	client.core.walletMtx.Unlock()
}

var _ comms.WsConn = (*tConn)(nil)

type tConn struct {
	comms.WsConn
	requestFilter atomic.Value // func(route string) error
	sendFilter    atomic.Value // func(msg *msgjson.Message) *msgjson.Message
}

// withRequestFilter installs a request filter and returns a cleanup function
// that removes it.
func (tc *tConn) withRequestFilter(f func(string) error) func() {
	tc.requestFilter.Store(f)
	return func() { tc.requestFilter.Store(func(string) error { return nil }) }
}

// withSendFilter installs a send filter and returns a cleanup function that
// removes it.
func (tc *tConn) withSendFilter(f func(*msgjson.Message) *msgjson.Message) func() {
	tc.sendFilter.Store(f)
	return func() { tc.sendFilter.Store(func(msg *msgjson.Message) *msgjson.Message { return msg }) }
}

func (tc *tConn) Send(msg *msgjson.Message) error {
	if fi := tc.sendFilter.Load(); fi != nil {
		msg = fi.(func(*msgjson.Message) *msgjson.Message)(msg)
	}
	return tc.WsConn.Send(msg)
}

func (tc *tConn) Request(msg *msgjson.Message, respHandler func(*msgjson.Message)) error {
	return tc.RequestWithTimeout(msg, respHandler, time.Minute, func() {})
}

func (tc *tConn) RequestWithTimeout(msg *msgjson.Message, respHandler func(*msgjson.Message), expireTime time.Duration, expire func()) error {
	if fi := tc.requestFilter.Load(); fi != nil {
		if err := fi.(func(string) error)(msg.Route); err != nil {
			return err
		}
	}
	return tc.WsConn.RequestWithTimeout(msg, respHandler, expireTime, expire)
}

func init() {
	if race {
		sleepFactor = 3
	}
}
