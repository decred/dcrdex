//go:build bnclive

package libxc

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
)

var (
	log       = dex.StdOutLogger("T", dex.LevelTrace)
	u, _      = user.Current()
	apiKey    = ""
	apiSecret = ""
)

func TestMain(m *testing.M) {
	if s := os.Getenv("SECRET"); s != "" {
		apiSecret = s
	}
	if k := os.Getenv("KEY"); k != "" {
		apiKey = k
	}

	m.Run()
}

func tNewBinance(t *testing.T, network dex.Network) *binance {
	return newBinance(apiKey, apiSecret, log, network, true)
}

type spoofDriver struct {
	cFactor uint64
}

func (drv *spoofDriver) Open(*asset.WalletConfig, dex.Logger, dex.Network) (asset.Wallet, error) {
	return nil, nil
}

func (drv *spoofDriver) DecodeCoinID(coinID []byte) (string, error) {
	return "", nil
}

func (drv *spoofDriver) Info() *asset.WalletInfo {
	return &asset.WalletInfo{
		UnitInfo: dex.UnitInfo{
			Conventional: dex.Denomination{
				ConversionFactor: drv.cFactor,
			},
		},
	}
}

func init() {
	asset.Register(42, &spoofDriver{cFactor: 1e9})  // dcr
	asset.Register(60, &spoofDriver{cFactor: 1e9})  // eth
	asset.Register(966, &spoofDriver{cFactor: 1e9}) // matic
	asset.Register(0, &spoofDriver{cFactor: 1e8})   // btc
	asset.RegisterToken(60001, &dex.Token{
		ParentID: 60,
		Name:     "USDC",
		UnitInfo: dex.UnitInfo{
			Conventional: dex.Denomination{
				ConversionFactor: 1e6,
			},
		},
	}, &asset.WalletDefinition{}, dex.Mainnet, dex.Testnet, dex.Simnet)
	asset.RegisterToken(966002, &dex.Token{
		ParentID: 966,
		Name:     "WETH",
		UnitInfo: dex.UnitInfo{
			Conventional: dex.Denomination{
				ConversionFactor: 1e9,
			},
		},
	}, &asset.WalletDefinition{}, dex.Mainnet, dex.Testnet, dex.Simnet)
}

func TestConnect(t *testing.T) {
	bnc := tNewBinance(t, dex.Simnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	balance, err := bnc.Balance(60)
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}
	t.Logf("usdc balance: %v", balance)

	balance, err = bnc.Balance(0)
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}
	t.Logf("btc balance: %v", balance)
}

// This may fail due to balance being to low. You can try switching the side
// of the trade or the qty.
func TestTrade(t *testing.T) {
	bnc := tNewBinance(t, dex.Mainnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()
	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	updates, unsubscribe, updaterID := bnc.SubscribeTradeUpdates()
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
	tradeID, err := bnc.Trade(ctx, 60, 0, false, 6000e2, 1e7, updaterID)
	if err != nil {
		t.Fatalf("trade error: %v", err)
	}

	if true { // Cancel the trade
		time.Sleep(1 * time.Second)
		err = bnc.CancelTrade(ctx, 60, 0, tradeID)
		if err != nil {
			t.Fatalf("error cancelling trade: %v", err)
		}
	}

	wg.Wait()
}

func TestCancelTrade(t *testing.T) {
	tradeID := "42641326270691d752e000000001"

	bnc := tNewBinance(t, dex.Testnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()
	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	err = bnc.CancelTrade(ctx, 60, 0, tradeID)
	if err != nil {
		t.Fatalf("error cancelling trade: %v", err)
	}
}

func TestMarkets(t *testing.T) {
	bnc := tNewBinance(t, dex.Mainnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	markets, err := bnc.Markets()
	if err != nil {
		t.Fatalf("failed to load markets")
	}

	for _, market := range markets {
		t.Logf("%v - %v", dex.BipIDSymbol(market.BaseID), dex.BipIDSymbol(market.QuoteID))
	}
}

func TestVWAP(t *testing.T) {
	bnc := tNewBinance(t, dex.Testnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()
	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	err = bnc.SubscribeMarket(ctx, 60, 0)
	if err != nil {
		t.Fatalf("failed to subscribe to market: %v", err)
	}

	time.Sleep(10 * time.Second)
	avg, extrema, filled, err := bnc.VWAP(60, 0, true, 2e9)
	if err != nil {
		t.Fatalf("VWAP failed: %v", err)
	}

	t.Logf("avg: %v, extrema: %v, filled: %v", avg, extrema, filled)

	err = bnc.SubscribeMarket(ctx, 60, 0)
	if err != nil {
		t.Fatalf("failed to subscribe to market: %v", err)
	}
	time.Sleep(2 * time.Second)

	avg, extrema, filled, err = bnc.VWAP(60, 0, true, 2e9)
	if err != nil {
		t.Fatalf("VWAP failed: %v", err)
	}

	t.Logf("avg: %v, extrema: %v, filled: %v", avg, extrema, filled)

	bnc.UnsubscribeMarket(60, 0)

	avg, extrema, filled, err = bnc.VWAP(60, 0, true, 2e9)
	if err != nil {
		t.Fatalf("VWAP failed: %v", err)
	}

	t.Logf("avg: %v, extrema: %v, filled: %v", avg, extrema, filled)

	bnc.UnsubscribeMarket(60, 0)
	if err != nil {
		t.Fatalf("error unsubscribing market")
	}
}

func TestWithdrawal(t *testing.T) {
	bnc := tNewBinance(t, dex.Mainnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	onComplete := func(amt uint64, txID string) {
		t.Logf("withdrawal complete: %v, %v", amt, txID)
		wg.Done()
	}

	err = bnc.Withdraw(ctx, 966, 2e10, "", onComplete)
	if err != nil {
		fmt.Printf("withdrawal error: %v", err)
		return
	}

	wg.Wait()
}

func TestConfirmDeposit(t *testing.T) {
	bnc := tNewBinance(t, dex.Mainnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	onComplete := func(success bool, amount uint64) {
		t.Logf("deposit complete: %v, %v", success, amount)
		wg.Done()
	}

	bnc.ConfirmDeposit(ctx, "", onComplete)

	wg.Wait()
}

func TestGetDepositAddress(t *testing.T) {
	bnc := tNewBinance(t, dex.Mainnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	addr, err := bnc.GetDepositAddress(ctx, 966)
	if err != nil {
		t.Fatalf("getDepositAddress error: %v", err)
	}

	t.Logf("deposit address: %v", addr)
}

func TestBalances(t *testing.T) {
	bnc := tNewBinance(t, dex.Mainnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	balance, err := bnc.Balance(966)
	if err != nil {
		t.Fatalf("balances error: %v", err)
	}

	t.Logf("%+v", balance)
}
