//go:build bnclive

package libxc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	_ "decred.org/dcrdex/client/asset/importall"
	"decred.org/dcrdex/client/mm/libxc/bntypes"
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

func tNewBinance(t *testing.T, net dex.Network) *binance {
	cfg := &CEXConfig{
		Net:       net,
		APIKey:    apiKey,
		SecretKey: apiSecret,
		Logger:    log,
		Notify: func(n interface{}) {
			log.Infof("Notification sent: %+v", n)
		},
	}
	const binanceUS = true
	return newBinance(cfg, binanceUS)
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
	bnc := tNewBinance(t, dex.Testnet)
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

	trade, err := bnc.Trade(ctx, 60, 60001, false, 3600e5, 1e7, updaterID)
	if err != nil {
		t.Fatalf("trade error: %v", err)
	}

	if false { // Cancel the trade
		time.Sleep(1 * time.Second)
		err = bnc.CancelTrade(ctx, 60, 0, trade.ID)
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

func TestMatchedMarkets(t *testing.T) {
	bnc := tNewBinance(t, dex.Mainnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	markets, err := bnc.Markets(ctx)
	if err != nil {
		t.Fatalf("failed to load markets")
	}

	for _, market := range markets {
		fmt.Printf("%s_%s \n", dex.BipIDSymbol(market.BaseID), dex.BipIDSymbol(market.QuoteID))
	}
}

func TestVWAP(t *testing.T) {
	bnc := tNewBinance(t, dex.Mainnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()
	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	err = bnc.SubscribeMarket(ctx, 60, 60001)
	if err != nil {
		t.Fatalf("failed to subscribe to market: %v", err)
	}

	err = bnc.SubscribeMarket(ctx, 60, 0)
	if err != nil {
		t.Fatalf("failed to subscribe to market: %v", err)
	}

	time.Sleep(30 * time.Second)

	avg, extrema, filled, err := bnc.VWAP(60, 0, true, 2e9)
	if err != nil {
		t.Fatalf("VWAP failed: %v", err)
	}
	t.Logf("ethbtc - avg: %v, extrema: %v, filled: %v", avg, extrema, filled)

	avg, extrema, filled, err = bnc.VWAP(60, 60001, true, 2e9)
	if err != nil {
		t.Fatalf("VWAP failed: %v", err)
	}
	t.Logf("ethusdc - avg: %v, extrema: %v, filled: %v", avg, extrema, filled)

	err = bnc.SubscribeMarket(ctx, 60, 0)
	if err != nil {
		t.Fatalf("failed to subscribe to market: %v", err)
	}

	avg, extrema, filled, err = bnc.VWAP(60, 0, true, 2e9)
	if err != nil {
		t.Fatalf("VWAP failed: %v", err)
	}

	t.Logf("ethbtc - avg: %v, extrema: %v, filled: %v", avg, extrema, filled)

	bnc.UnsubscribeMarket(60, 0)

	avg, extrema, filled, err = bnc.VWAP(60, 0, true, 2e9)
	if err != nil {
		t.Fatalf("VWAP failed: %v", err)
	}

	t.Logf("avg: %v, extrema: %v, filled: %v", avg, extrema, filled)

	err = bnc.UnsubscribeMarket(60, 0)
	if err != nil {
		t.Fatalf("error unsubscribing market")
	}

	_, _, _, err = bnc.VWAP(60, 0, true, 2e9)
	if err == nil {
		t.Fatalf("error should be returned since all subscribers have unsubscribed")
	}
}

func TestSubscribeMarket(t *testing.T) {
	bnc := tNewBinance(t, dex.Testnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()
	wg, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	err = bnc.SubscribeMarket(ctx, 60, 0)
	if err != nil {
		t.Fatalf("failed to subscribe to market: %v", err)
	}

	wg.Wait()
}

func TestWithdrawal(t *testing.T) {
	bnc := tNewBinance(t, dex.Mainnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	withdrawalID, err := bnc.Withdraw(ctx, 966, 2e10, "")
	if err != nil {
		fmt.Printf("withdrawal error: %v", err)
		return
	}

	t.Logf("withdrawalID: %v", withdrawalID)
}

func TestConfirmDeposit(t *testing.T) {
	bnc := tNewBinance(t, dex.Mainnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	confirmed, amt := bnc.ConfirmDeposit(ctx, &DepositData{})
	t.Logf("confirmed: %v, amt: %v", confirmed, amt)
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
	bnc := tNewBinance(t, dex.Testnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	balance, err := bnc.Balance(0)
	if err != nil {
		t.Fatalf("balances error: %v", err)
	}

	t.Logf("%+v", balance)
}

func TestGetCoinInfo(t *testing.T) {
	bnc := tNewBinance(t, dex.Mainnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	coins := make([]*bntypes.CoinInfo, 0)
	err := bnc.getAPI(ctx, "/sapi/v1/capital/config/getall", nil, true, true, &coins)
	if err != nil {
		t.Fatalf("error getting binance coin info: %v", err)
	}

	coinLookup := make(map[string]bool)
	for _, a := range asset.Assets() {
		coinLookup[a.Info.UnitInfo.Conventional.Unit] = true
		for _, tkn := range a.Tokens {
			coinLookup[tkn.UnitInfo.Conventional.Unit] = true
		}
	}

	for _, c := range coins {
		if !coinLookup[c.Coin] {
			continue
		}
		networks := make([]string, 0)
		for _, n := range c.NetworkList {
			if !n.DepositEnable || !n.WithdrawEnable {
				fmt.Printf("%s on network %s not withdrawing and/or depositing. withdraw = %t, deposit = %t\n",
					c.Coin, n.Network, n.WithdrawEnable, n.DepositEnable)
			}
			networks = append(networks, n.Network)
		}
		fmt.Printf("%q networks: %+v \n", c.Coin, networks)
	}
}

func TestTradeStatus(t *testing.T) {
	bnc := tNewBinance(t, dex.Testnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	trade, err := bnc.TradeStatus(ctx, "eb6b6e1177213643142700000001", 60, 60001)
	if err != nil {
		t.Fatalf("trade status error: %v", err)
	}

	t.Logf("trade status: %+v", trade)
}

func TestMarkets(t *testing.T) {
	// Need keys for getCoinInfo
	bnc := tNewBinance(t, dex.Testnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	err := bnc.getCoinInfo(ctx)
	if err != nil {
		t.Fatalf("error getting coin info: %v", err)
	}

	mkts, err := bnc.Markets(ctx)
	if err != nil {
		t.Fatalf("error getting markets: %v", err)
	}

	b, _ := json.MarshalIndent(mkts, "", "    ")
	fmt.Println("##### Market Data:", string(b))
}
