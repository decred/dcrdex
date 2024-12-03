//go:build bnclive

package libxc

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	_ "decred.org/dcrdex/client/asset/importall"
	"decred.org/dcrdex/client/mm/libxc/bntypes"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
)

var (
	log       = dex.StdOutLogger("T", dex.LevelTrace)
	binanceUS = true
	net       = dex.Mainnet
	apiKey    string
	apiSecret string
	baseID    uint64
	quoteID   uint64
	rate      float64
	qty       float64
	sell      bool
	tradeID   string
	assetID   uint64
	txID      string
	addr      string
)

func TestMain(m *testing.M) {
	var global, testnet bool
	flag.BoolVar(&global, "global", false, "use Binance global")
	flag.BoolVar(&testnet, "testnet", false, "use testnet")
	flag.Uint64Var(&baseID, "base", ^uint64(0), "base asset ID")
	flag.Uint64Var(&quoteID, "quote", ^uint64(0), "quote asset ID")
	flag.Float64Var(&rate, "rate", -1, "rate")
	flag.Float64Var(&qty, "qty", -1, "qty")
	flag.StringVar(&tradeID, "trade", "", "trade ID")
	flag.BoolVar(&sell, "sell", false, "sell")
	flag.Uint64Var(&assetID, "asset", ^uint64(0), "asset ID")
	flag.StringVar(&txID, "tx", "", "tx ID")
	flag.StringVar(&addr, "addr", "", "address")
	flag.Parse()

	if global {
		binanceUS = false
	}
	if testnet {
		net = dex.Testnet
	}

	if s := os.Getenv("SECRET"); s != "" {
		apiSecret = s
	}
	if k := os.Getenv("KEY"); k != "" {
		apiKey = k
	}

	m.Run()
}

func tNewBinance() *binance {
	cfg := &CEXConfig{
		Net:       net,
		APIKey:    apiKey,
		SecretKey: apiSecret,
		Logger:    log,
		Notify: func(n interface{}) {
			log.Infof("Notification sent: %+v", n)
		},
	}
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

func TestPlaceTrade(t *testing.T) {
	if baseID == ^uint64(0) || quoteID == ^uint64(0) || rate < 0 || qty < 0 {
		t.Fatalf("baseID, quoteID, rate, or qty not set")
	}

	bnc := tNewBinance()
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

	baseUI, _ := asset.UnitInfo(uint32(baseID))
	quoteUI, _ := asset.UnitInfo(uint32(quoteID))
	msgRate := calc.MessageRate(rate, baseUI, quoteUI)
	msgQty := uint64(math.Round(qty * float64(baseUI.Conventional.ConversionFactor)))

	t.Logf("msgRate: %v, msgQty: %v", msgRate, msgQty)
	trade, err := bnc.Trade(ctx, uint32(baseID), uint32(quoteID), false, msgRate, msgQty, updaterID)
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
	if tradeID == "" {
		t.Fatalf("tradeID not set")
	}
	if baseID == ^uint64(0) || quoteID == ^uint64(0) {
		t.Fatalf("baseID or quoteID not set")
	}

	bnc := tNewBinance()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()
	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	err = bnc.CancelTrade(ctx, uint32(baseID), uint32(quoteID), tradeID)
	if err != nil {
		t.Fatalf("error cancelling trade: %v", err)
	}
}

func TestMatchedMarkets(t *testing.T) {
	bnc := tNewBinance()
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
		fmt.Printf("%s_%s %d %d\n", dex.BipIDSymbol(market.BaseID), dex.BipIDSymbol(market.QuoteID), market.BaseMinWithdraw, market.QuoteMinWithdraw)
	}
}

func TestVWAP(t *testing.T) {
	bnc := tNewBinance()
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
	if baseID == ^uint64(0) || quoteID == ^uint64(0) {
		t.Fatalf("baseID or quoteID not set")
	}

	bnc := tNewBinance()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()
	wg, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	err = bnc.SubscribeMarket(ctx, uint32(baseID), uint32(quoteID))
	if err != nil {
		t.Fatalf("failed to subscribe to market: %v", err)
	}

	wg.Wait()
}

func TestWithdrawal(t *testing.T) {
	if assetID == ^uint64(0) {
		t.Fatalf("assetID not set")
	}
	if qty < 0 {
		t.Fatalf("qty not set")
	}
	if addr == "" {
		t.Fatalf("addr not set")
	}

	bnc := tNewBinance()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	ui, _ := asset.UnitInfo(uint32(assetID))
	msgQty := uint64(math.Round(qty * float64(ui.Conventional.ConversionFactor)))

	t.Logf("msgQty: %v", msgQty)
	withdrawalID, err := bnc.Withdraw(ctx, uint32(assetID), msgQty, addr)
	if err != nil {
		fmt.Printf("withdrawal error: %v", err)
		return
	}

	t.Logf("withdrawalID: %v", withdrawalID)
}

func TestConfirmDeposit(t *testing.T) {
	if assetID == ^uint64(0) {
		t.Fatalf("assetID not set")
	}
	if txID == "" {
		t.Fatalf("txID not set")
	}

	bnc := tNewBinance()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	confirmed, amt := bnc.ConfirmDeposit(ctx, &DepositData{
		AssetID: uint32(assetID),
		TxID:    txID,
	})

	t.Logf("Confirmed: %v, Amt: %v", confirmed, amt)
}

func TestGetDepositAddress(t *testing.T) {
	if assetID == ^uint64(0) {
		t.Fatalf("assetID not set")
	}

	bnc := tNewBinance()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	addr, err := bnc.GetDepositAddress(ctx, uint32(assetID))
	if err != nil {
		t.Fatalf("getDepositAddress error: %v", err)
	}

	t.Logf("Deposit Address: %v", addr)
}

func TestBalanceIndividually(t *testing.T) {
	if assetID == ^uint64(0) {
		t.Fatalf("assetID not set")
	}

	bnc := tNewBinance()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	balance, err := bnc.Balance(uint32(assetID))
	if err != nil {
		t.Fatalf("balances error: %v", err)
	}

	t.Logf("%+v", balance)
}

func TestBalances(t *testing.T) {
	bnc := tNewBinance()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	balances, err := bnc.Balances(ctx)
	if err != nil {
		t.Fatalf("balances error: %v", err)
	}

	for assetID, b := range balances {
		t.Logf("%s: %+v", dex.BipIDSymbol(assetID), b)
	}
}

func TestGetCoinInfo(t *testing.T) {
	bnc := tNewBinance()
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
		networkMins := make([]string, 0)
		for _, n := range c.NetworkList {
			if !n.DepositEnable || !n.WithdrawEnable {
				fmt.Printf("%s on network %s not withdrawing and/or depositing. withdraw = %t, deposit = %t\n",
					c.Coin, n.Network, n.WithdrawEnable, n.DepositEnable)
			}
			networkMins = append(networkMins, fmt.Sprintf("{net: %s, min_withdraw: %.8f, withdraw_fee: %.8f}", n.Network, n.WithdrawMin, n.WithdrawFee))
		}
		fmt.Printf("%q network mins: %+v \n", c.Coin, strings.Join(networkMins, ", "))
	}
}

func TestTradeStatus(t *testing.T) {
	if tradeID == "" {
		t.Fatalf("tradeID not set")
	}
	if baseID == ^uint64(0) || quoteID == ^uint64(0) {
		t.Fatalf("baseID or quoteID not set")
	}

	bnc := tNewBinance()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	trade, err := bnc.TradeStatus(ctx, tradeID, uint32(baseID), uint32(quoteID))
	if err != nil {
		t.Fatalf("trade status error: %v", err)
	}

	t.Logf("trade status: %+v", trade)
}

func TestMarkets(t *testing.T) {
	// Need keys for getCoinInfo
	bnc := tNewBinance()
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
