//go:build cblive

package libxc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/mm/libxc/cbtypes"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"github.com/davecgh/go-spew/spew"

	_ "decred.org/dcrdex/client/asset/bch"     // register bch asset
	_ "decred.org/dcrdex/client/asset/btc"     // register btc asset
	_ "decred.org/dcrdex/client/asset/dash"    // register dash asset
	_ "decred.org/dcrdex/client/asset/dcr"     // register dcr asset
	_ "decred.org/dcrdex/client/asset/dgb"     // register dgb asset
	_ "decred.org/dcrdex/client/asset/doge"    // register doge asset
	_ "decred.org/dcrdex/client/asset/eth"     // register eth asset
	_ "decred.org/dcrdex/client/asset/firo"    // register firo asset
	_ "decred.org/dcrdex/client/asset/ltc"     // register ltc asset
	_ "decred.org/dcrdex/client/asset/polygon" // register polygon asset
	_ "decred.org/dcrdex/client/asset/zcl"     // register zcl asset
	_ "decred.org/dcrdex/client/asset/zec"     // register zec asset
)

var tCtx context.Context

func TestMain(m *testing.M) {
	asset.SetNetwork(dex.Mainnet)
	var shutdown func()
	tCtx, shutdown = context.WithCancel(context.Background())
	doIt := func() int {
		defer shutdown()
		return m.Run()
	}
	os.Exit(doIt())
}

func tNewCoinbase(t *testing.T) (*coinbase, func()) {
	credsPath := os.Getenv("CBCREDS")
	if credsPath == "" {
		t.Fatalf("No CBCREDS environmental variable found")
	}
	b, err := os.ReadFile(dex.CleanAndExpandPath(credsPath))
	if err != nil {
		t.Fatalf("error reading credentials")
	}

	var creds struct {
		Name       string `json:"name"`
		PrivateKey string `json:"privateKey"`
	}
	if err := json.Unmarshal(b, &creds); err != nil {
		t.Fatalf("error unmarshaling credentials: %v", err)
	}

	if creds.Name == "" || creds.PrivateKey == "" {
		t.Fatalf("Incomplete credentials")
	}

	c, err := newCoinbase(&CEXConfig{
		Net:       dex.Mainnet,
		APIKey:    creds.Name,
		SecretKey: creds.PrivateKey,
		Logger:    dex.StdOutLogger("T", dex.LevelInfo),
		Notify:    func(interface{}) {},
	})
	if err != nil {
		t.Fatalf("error creating coinbase: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.ctx = ctx
	return c, cancel
}

// Pass in ASSET_ID, AMOUNT, and ADDRESS env vars.
func TestWithdraw(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	cm := dex.NewConnectionMaster(c)
	if err := cm.ConnectOnce(c.ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	defer func() {
		cm.Disconnect()
		cm.Wait()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	assetIDStr := os.Getenv("ASSET_ID")
	if assetIDStr == "" {
		t.Skip("ASSET_ID env var not set")
	}
	assetID, err := strconv.ParseUint(assetIDStr, 10, 32)
	if err != nil {
		t.Fatalf("Invalid ASSET_ID: %v", err)
	}
	ui, err := asset.UnitInfo(uint32(assetID))
	if err != nil {
		t.Fatalf("UnitInfo error: %v", err)
	}

	amountStr := os.Getenv("AMOUNT")
	if amountStr == "" {
		t.Skip("AMOUNT env var not set")
	}
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		t.Fatalf("Invalid AMOUNT: %v", err)
	}
	qty := uint64(math.Round(amount * float64(ui.Conventional.ConversionFactor)))

	address := os.Getenv("ADDRESS")
	if address == "" {
		t.Skip("ADDRESS env var not set")
	}

	withdrawalID, withdrawnAmount, err := c.Withdraw(ctx, uint32(assetID), qty, address)
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println("Withdrawal ID:", withdrawalID, "Amount:", withdrawnAmount)
}

// Pass in WITHDRAWAL_ID and ASSET_ID env vars.
func TestConfirmWithdrawal(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	cm := dex.NewConnectionMaster(c)
	if err := cm.ConnectOnce(c.ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	defer func() {
		cm.Disconnect()
		cm.Wait()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	withdrawalID := os.Getenv("WITHDRAWAL_ID")
	if withdrawalID == "" {
		t.Skip("WITHDRAWAL_ID env var not set")
	}

	assetIDStr := os.Getenv("ASSET_ID")
	if assetIDStr == "" {
		t.Skip("ASSET_ID env var not set")
	}
	assetID, err := strconv.ParseUint(assetIDStr, 10, 32)
	if err != nil {
		t.Fatalf("Invalid ASSET_ID: %v", err)
	}

	for {
		amt, txID, err := c.ConfirmWithdrawal(ctx, withdrawalID, uint32(assetID))
		if err != nil && !errors.Is(err, ErrWithdrawalPending) {
			t.Fatalf("ConfirmWithdrawal error: %v", err)
		}

		if err == nil {
			fmt.Println("Withdrawal confirmed:", amt, "TXID:", txID)
			return
		}

		fmt.Println("withdrawal pending...")
		time.Sleep(10 * time.Second)
	}
}

func TestMarkets(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	cm := dex.NewConnectionMaster(c)
	if err := cm.ConnectOnce(c.ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	defer func() {
		cm.Disconnect()
		cm.Wait()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mkts, err := c.Markets(ctx)
	if err != nil {
		t.Fatalf("Markets error: %v", err)
	}

	for _, mkt := range mkts {
		fmt.Printf("Market: %d-%d - %+v\n", mkt.BaseID, mkt.QuoteID, mkt.Day)
	}
}

func TestOrderbookSubscription(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	cm := dex.NewConnectionMaster(c)
	if err := cm.ConnectOnce(c.ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	defer func() {
		cm.Disconnect()
		cm.Wait()
	}()

	wg := sync.WaitGroup{}

	runTest := func(baseID, quoteID uint32, sell bool, vwapQty, invVWAPQty uint64) {
		defer wg.Done()

		if err := c.SubscribeMarket(c.ctx, baseID, quoteID); err != nil {
			t.Errorf("Error subscribing to order book: %v", err)
			return
		}

		time.Sleep(time.Second * 10)

		for i := 0; i < 20; i++ {
			select {
			case <-time.After(time.Second * 1):
				vwap, extrema, filled, err := c.VWAP(baseID, quoteID, sell, vwapQty)
				if err != nil {
					t.Errorf("VWAP error: %v", err)
					return
				}

				midGap := c.MidGap(baseID, quoteID)

				invVWAP, invExtrema, invFilled, err := c.InvVWAP(baseID, quoteID, sell, invVWAPQty)
				if err != nil {
					t.Errorf("VWAP error: %v", err)
					return
				}

				vwapLog := fmt.Sprintf("%d-%d VWAP: %v, Extrema: %v, Filled: %v, MidGap: %v\n", baseID, quoteID, vwap, extrema, filled, midGap)
				invVwapLog := fmt.Sprintf("%d-%d invVWAP: %v, Extrema: %v, Filled: %v, MidGap: %v\n", baseID, quoteID, invVWAP, invExtrema, invFilled, midGap)
				fmt.Printf("=========================\n%s%s\n", vwapLog, invVwapLog)
			case <-c.ctx.Done():
				return
			}
		}

		if err := c.SubscribeMarket(c.ctx, baseID, quoteID); err != nil {
			t.Errorf("Error subscribing to order book: %v", err)
			return
		}

		c.UnsubscribeMarket(baseID, quoteID)

		_, _, _, err := c.VWAP(baseID, quoteID, sell, 1e8)
		if err != nil {
			t.Errorf("VWAP error: %v", err)
			return
		}

		c.UnsubscribeMarket(baseID, quoteID)

		_, _, _, err = c.VWAP(baseID, quoteID, sell, 1e8)
		if err == nil {
			t.Errorf("Expected error after unsubscribing")
			return
		}
	}

	wg.Add(2)

	go runTest(0, 60001, true, 10e8, 2000e6)
	go runTest(60, 60001, true, 10e9, 2000e6)

	wg.Wait()
}

func TestBook(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	cm := dex.NewConnectionMaster(c)
	if err := cm.ConnectOnce(c.ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	defer func() {
		cm.Disconnect()
		cm.Wait()
	}()

	baseIDStr := os.Getenv("BASE_ID")
	if baseIDStr == "" {
		t.Skip("BASE_ID env var not set")
	}
	baseID, err := strconv.ParseUint(baseIDStr, 10, 32)
	if err != nil {
		t.Fatalf("Invalid BASE_ID: %v", err)
	}

	quoteIDStr := os.Getenv("QUOTE_ID")
	if quoteIDStr == "" {
		t.Skip("QUOTE_ID env var not set")
	}
	quoteID, err := strconv.ParseUint(quoteIDStr, 10, 32)
	if err != nil {
		t.Fatalf("Invalid QUOTE_ID: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = c.SubscribeMarket(ctx, uint32(baseID), uint32(quoteID))
	if err != nil {
		t.Fatalf("error subscribing to market: %v", err)
	}
	defer c.UnsubscribeMarket(uint32(baseID), uint32(quoteID))

	endTime := time.Now().Add(time.Hour)
	for time.Now().Before(endTime) {
		select {
		case <-time.After(10 * time.Second):
			buys, sells, err := c.Book(uint32(baseID), uint32(quoteID))
			if err != nil {
				t.Errorf("error getting book: %v", err)
				continue
			}
			t.Logf("~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~")
			t.Logf("Buys:")
			for i, buy := range buys {
				if i > 5 {
					break
				}
				t.Logf("  %+v", buy)
			}
			t.Logf("Sells:")
			for i, sell := range sells {
				if i > 5 {
					break
				}
				t.Logf("  %+v", sell)
			}
		case <-ctx.Done():
			return
		}
	}
}

// Pass in ASSET_ID env var.
func TestGetDepositAddress(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	cm := dex.NewConnectionMaster(c)
	if err := cm.ConnectOnce(c.ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	defer func() {
		cm.Disconnect()
		cm.Wait()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	assetIDStr := os.Getenv("ASSET_ID")
	if assetIDStr == "" {
		t.Skip("ASSET_ID env var not set")
	}
	assetID, err := strconv.ParseUint(assetIDStr, 10, 32)
	if err != nil {
		t.Fatalf("Invalid ASSET_ID: %v", err)
	}

	addr, err := c.GetDepositAddress(ctx, uint32(assetID))
	if err != nil {
		t.Fatalf("GetDepositAddress error: %v", err)
	}

	fmt.Printf("Deposit address: %s\n", addr)
}

// Pass in ASSET_ID and TX_ID env vars.
func TestConfirmDeposit(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	cm := dex.NewConnectionMaster(c)
	if err := cm.ConnectOnce(c.ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	defer func() {
		cm.Disconnect()
		cm.Wait()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	assetIDStr := os.Getenv("ASSET_ID")
	if assetIDStr == "" {
		t.Skip("ASSET_ID env var not set")
	}
	assetID, err := strconv.ParseUint(assetIDStr, 10, 32)
	if err != nil {
		t.Fatalf("Invalid ASSET_ID: %v", err)
	}

	txIDStr := os.Getenv("TX_ID")
	if txIDStr == "" {
		t.Skip("TX_ID env var not set")
	}

	for {
		complete, amt := c.ConfirmDeposit(ctx, &DepositData{
			AssetID: uint32(assetID),
			TxID:    txIDStr,
		})
		if complete {
			fmt.Printf("Deposit confirmed: %d\n", amt)
			return
		}

		time.Sleep(10 * time.Second)
	}
}

func TestBalanceIndividually(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	cm := dex.NewConnectionMaster(c)
	if err := cm.ConnectOnce(c.ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	defer func() {
		cm.Disconnect()
		cm.Wait()
	}()

	accounts := c.accounts.Load().(map[string]*cbtypes.Account)
	for assetID, ticker := range c.idTicker {
		_, supported := accounts[ticker]
		if !supported {
			continue
		}
		b, err := c.Balance(assetID)
		if err != nil {
			t.Fatalf("Balance error: %v", err)
		}

		fmt.Printf("%s Balance = %+v \n", dex.BipIDSymbol(assetID), b)
	}
}

func TestBalances(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	balances, err := c.Balances(context.Background())
	if err != nil {
		t.Fatalf("Balances error: %v", err)
	}

	for assetID, balance := range balances {
		fmt.Printf("%s Balance = %+v \n", dex.BipIDSymbol(assetID), balance)
	}
}

// Pass in BASE_ID, QUOTE_ID, RATE or MARKET, AMOUNT, SELL, and CANCEL_TRADE env vars.
func TestPlaceTrade(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	cm := dex.NewConnectionMaster(c)
	if err := cm.ConnectOnce(c.ctx); err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	defer func() {
		cm.Disconnect()
		cm.Wait()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	baseIDStr := os.Getenv("BASE_ID")
	if baseIDStr == "" {
		t.Skip("BASE_ID env var not set")
	}
	baseID, err := strconv.ParseUint(baseIDStr, 10, 32)
	if err != nil {
		t.Fatalf("Invalid BASE_ID: %v", err)
	}

	quoteIDStr := os.Getenv("QUOTE_ID")
	if quoteIDStr == "" {
		t.Skip("QUOTE_ID env var not set")
	}
	quoteID, err := strconv.ParseUint(quoteIDStr, 10, 32)
	if err != nil {
		t.Fatalf("Invalid QUOTE_ID: %v", err)
	}

	marketStr := os.Getenv("MARKET")
	orderType := OrderTypeLimit
	if marketStr == "true" {
		orderType = OrderTypeMarket
	}

	rateStr := os.Getenv("RATE")
	if orderType == OrderTypeLimit && rateStr == "" {
		t.Fatalf("RATE env var not set")
	}

	var rate float64
	if orderType == OrderTypeLimit {
		rate, err = strconv.ParseFloat(rateStr, 64)
		if err != nil {
			t.Fatalf("Invalid RATE: %v", err)
		}
	}

	amountStr := os.Getenv("AMOUNT")
	if amountStr == "" {
		t.Skip("AMOUNT env var not set")
	}
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		t.Fatalf("Invalid AMOUNT: %v", err)
	}

	cancelTradeStr := os.Getenv("CANCEL_TRADE")
	cancelTrade := cancelTradeStr == "true"

	sellStr := os.Getenv("SELL")
	sell := sellStr == "true"

	bui, err := asset.UnitInfo(uint32(baseID))
	if err != nil {
		t.Fatalf("UnitInfo error: %v", err)
	}

	qui, err := asset.UnitInfo(uint32(quoteID))
	if err != nil {
		t.Fatalf("UnitInfo error: %v", err)
	}

	msgRate := calc.MessageRate(rate, bui, qui)
	qty := uint64(math.Round(amount * float64(bui.Conventional.ConversionFactor)))

	wg := sync.WaitGroup{}
	wg.Add(1)
	updates, unsubscribe, updaterID := c.SubscribeTradeUpdates()
	defer unsubscribe()

	go func() {
		defer wg.Done()
		for {
			select {
			case tradeUpdate := <-updates:
				t.Logf("Trade Update: %+v", tradeUpdate)
				if tradeUpdate.Complete {
					// Sleep because context might get cancelled before
					// Trade returns.
					time.Sleep(1 * time.Second)
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	trade, err := c.Trade(ctx, uint32(baseID), uint32(quoteID), sell, msgRate, qty, orderType, updaterID)
	if err != nil {
		t.Fatalf("Trade error: %v", err)
	}

	spew.Dump(trade)

	if cancelTrade { // Cancel the trade
		time.Sleep(10 * time.Second)
		err = c.CancelTrade(ctx, uint32(baseID), uint32(quoteID), trade.ID)
		if err != nil {
			t.Fatalf("error cancelling trade: %v", err)
		}
	}

	wg.Wait()
}

// Pass in BASE_ID, QUOTE_ID, and TRADE_ID env vars.
func TestCancelTrade(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	baseIDStr := os.Getenv("BASE_ID")
	if baseIDStr == "" {
		t.Skip("BASE_ID env var not set")
	}
	baseID, err := strconv.ParseUint(baseIDStr, 10, 32)
	if err != nil {
		t.Fatalf("Invalid BASE_ID: %v", err)
	}

	quoteIDStr := os.Getenv("QUOTE_ID")
	if quoteIDStr == "" {
		t.Skip("QUOTE_ID env var not set")
	}
	quoteID, err := strconv.ParseUint(quoteIDStr, 10, 32)
	if err != nil {
		t.Fatalf("Invalid QUOTE_ID: %v", err)
	}

	tradeIDStr := os.Getenv("TRADE_ID")
	if tradeIDStr == "" {
		t.Skip("TRADE_ID env var not set")
	}

	err = c.CancelTrade(context.Background(), uint32(baseID), uint32(quoteID), tradeIDStr)
	if err != nil {
		t.Fatalf("error cancelling trade: %v", err)
	}
}

func TestTradeStatus(t *testing.T) {
	c, shutdown := tNewCoinbase(t)
	defer shutdown()

	baseIDStr := os.Getenv("BASE_ID")
	if baseIDStr == "" {
		t.Skip("BASE_ID env var not set")
	}
	baseID, err := strconv.ParseUint(baseIDStr, 10, 32)
	if err != nil {
		t.Fatalf("Invalid BASE_ID: %v", err)
	}

	quoteIDStr := os.Getenv("QUOTE_ID")
	if quoteIDStr == "" {
		t.Skip("QUOTE_ID env var not set")
	}
	quoteID, err := strconv.ParseUint(quoteIDStr, 10, 32)
	if err != nil {
		t.Fatalf("Invalid QUOTE_ID: %v", err)
	}

	tradeIDStr := os.Getenv("TRADE_ID")
	if tradeIDStr == "" {
		t.Skip("TRADE_ID env var not set")
	}

	trade, err := c.TradeStatus(context.Background(), tradeIDStr, uint32(baseID), uint32(quoteID))
	if err != nil {
		t.Fatalf("error getting trade status: %v", err)
	}

	spew.Dump(trade)
}
