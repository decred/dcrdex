//go:build cblive

package libxc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
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

	c, err := newCoinbase(creds.Name, creds.PrivateKey, dex.StdOutLogger("T", dex.LevelInfo), dex.Mainnet)
	if err != nil {
		t.Fatalf("error creating coinbase: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	c.ctx = ctx
	return c, cancel
}

func TestAccounts(t *testing.T) {
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

	accounts := c.accounts.Load().(map[string]*coinbaseAccount)
	for _, a := range accounts {
		fmt.Printf("%s ID = %s, Balance = %f \n", a.AvailableBalance.Currency, a.UUID, a.AvailableBalance.Value)
	}
}

func TestAssets(t *testing.T) {
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

	assets := c.assets.Load().(map[uint32]*coinbaseAsset)
	for _, a := range assets {
		fmt.Printf("%s Exponent = %v \n", a.Code, a.Exponent)
	}
}

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

	withdrawalID, amount, err := c.Withdraw(ctx, 966, 7e9, "")
	if err != nil {
		t.Fatal(err)
	}

	fmt.Println("Withdrawal ID:", withdrawalID, "Amount:", amount)
}

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

	withdrawalID := ""
	for {
		amt, txID, err := c.ConfirmWithdrawal(ctx, withdrawalID, 966)
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

	markets := c.markets.Load().(map[string]*cbMarket)
	for _, m := range markets {
		fmt.Printf("Market %s price %f has moved %s%% in the last 24 hours\n", m.ProductID, m.Price, m.DayPriceChangePctStr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mkts, err := c.Markets(ctx)
	if err != nil {
		t.Fatalf("Markets error: %v", err)
	}

	for _, mkt := range mkts {
		fmt.Println("Market:", mkt.BaseID, mkt.QuoteID)
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

	if err := c.SubscribeMarket(0 /* btc */, 60001 /* usdc.eth */); err != nil {
		t.Fatalf("Error subscribing to BTC-USDC order book: %v", err)
	}

	time.Sleep(time.Second * 10)

	for i := 0; i < 20; i++ {
		select {
		case <-time.After(time.Second * 1):
			vwap, extrema, filled, err := c.VWAP(0, 60001, true, 1e8)
			if err != nil {
				t.Fatalf("VWAP error: %v", err)
			}

			midGap := c.MidGap(0, 60001)

			fmt.Printf("VWAP: %v, Extrema: %v, Filled: %v, MidGap: %v\n", vwap, extrema, filled, midGap)
		case <-c.ctx.Done():
			return
		}
	}

	if err := c.SubscribeMarket(0 /* btc */, 60001 /* usdc.eth */); err != nil {
		t.Fatalf("Error subscribing to BTC-USDC order book: %v", err)
	}

	c.UnsubscribeMarket(0, 60001)

	_, _, _, err := c.VWAP(0, 60001, true, 1e8)
	if err != nil {
		t.Fatalf("VWAP error: %v", err)
	}

	c.UnsubscribeMarket(0, 60001)

	_, _, _, err = c.VWAP(0, 60001, true, 1e8)
	if err == nil {
		t.Fatalf("Expected error after unsubscribing")
	}
}

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

	addr, err := c.GetDepositAddress(ctx, 60001)
	if err != nil {
		t.Fatalf("GetDepositAddress error: %v", err)
	}

	fmt.Printf("Deposit address: %s\n", addr)
}

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

	depositData := &DepositData{
		AssetID: 966,
		TxID:    "",
	}

	for {
		complete, amt := c.ConfirmDeposit(ctx, depositData)
		if complete {
			fmt.Printf("Deposit confirmed: %d\n", amt)
			return
		}

		time.Sleep(10 * time.Second)
	}
}

func TestBalances(t *testing.T) {
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

	accounts := c.accounts.Load().(map[string]*coinbaseAccount)
	for assetID, ticker := range c.idTicker {
		_, supported := accounts[ticker]
		if !supported {
			continue
		}
		b, err := c.Balance(ctx, assetID)
		if err != nil {
			t.Fatalf("Balance error: %v", err)
		}

		fmt.Printf("%s Balance = %+v \n", dex.BipIDSymbol(assetID), b)
	}
}

func TestTrade(t *testing.T) {
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

	bui, err := asset.UnitInfo(0)
	if err != nil {
		t.Fatalf("UnitInfo error: %v", err)
	}

	qui, err := asset.UnitInfo(60001)
	if err != nil {
		t.Fatalf("UnitInfo error: %v", err)
	}

	msgRate := calc.MessageRate(72000, bui, qui)

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

	trade, err := c.Trade(ctx, 0, 60001, false, msgRate, 14285, updaterID)
	if err != nil {
		t.Fatalf("Trade error: %v", err)
	}

	spew.Println(trade)

	if false { // Cancel the trade
		time.Sleep(10 * time.Second)
		err = c.CancelTrade(ctx, 0, 60001, trade.ID)
		if err != nil {
			t.Fatalf("error cancelling trade: %v", err)
		}
	}

	wg.Wait()
}
