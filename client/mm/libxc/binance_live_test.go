//go:build bnclive && lgpl

package libxc

import (
	"context"
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
}

func TestConnect(t *testing.T) {
	bnc := tNewBinance(t, dex.Simnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	balance, err := bnc.Balance("eth")
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}
	t.Logf("usdc balance: %v", balance)

	balance, err = bnc.Balance("btc")
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}
	t.Logf("btc balance: %v", balance)
}

// This may fail due to balance being to low. You can try switching the side
// of the trade or the qty.
func TestTrade(t *testing.T) {
	bnc := tNewBinance(t, dex.Simnet)
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
					cancel()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	tradeID := bnc.GenerateTradeID()
	err = bnc.Trade(ctx, "eth", "btc", true, 6327e2, 1e8, updaterID, tradeID)
	if err != nil {
		t.Fatalf("trade error: %v", err)
	}

	wg.Wait()
}

func TestCancelTrade(t *testing.T) {
	tradeID := "d4d81cd45db6f8c229a100000001"

	bnc := tNewBinance(t, dex.Testnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()
	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	err = bnc.CancelTrade(ctx, "eth", "btc", tradeID)
	if err != nil {
		t.Fatalf("error cancelling trade: %v", err)
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

	err = bnc.SubscribeMarket(ctx, "eth", "btc")
	if err != nil {
		t.Fatalf("failed to subscribe to market: %v", err)
	}

	time.Sleep(10 * time.Second)
	avg, extrema, filled, err := bnc.VWAP("eth", "btc", true, 2e9)
	if err != nil {
		t.Fatalf("VWAP failed: %v", err)
	}

	t.Logf("avg: %v, extrema: %v, filled: %v", avg, extrema, filled)

	err = bnc.SubscribeMarket(ctx, "eth", "btc")
	if err != nil {
		t.Fatalf("failed to subscribe to market: %v", err)
	}
	time.Sleep(2 * time.Second)

	avg, extrema, filled, err = bnc.VWAP("eth", "btc", true, 2e9)
	if err != nil {
		t.Fatalf("VWAP failed: %v", err)
	}

	t.Logf("avg: %v, extrema: %v, filled: %v", avg, extrema, filled)

	bnc.UnsubscribeMarket("eth", "btc")

	avg, extrema, filled, err = bnc.VWAP("eth", "btc", true, 2e9)
	if err != nil {
		t.Fatalf("VWAP failed: %v", err)
	}

	t.Logf("avg: %v, extrema: %v, filled: %v", avg, extrema, filled)

	bnc.UnsubscribeMarket("eth", "btc")
	if err != nil {
		t.Fatalf("error unsubscribing market")
	}
}
