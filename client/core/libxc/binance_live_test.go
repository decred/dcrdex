//go:build bnclive && lgpl

package libxc

import (
	"context"
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/dex"
)

var (
	log           = dex.StdOutLogger("T", dex.LevelTrace)
	u, _          = user.Current()
	testCredsPath = filepath.Join(u.HomeDir, ".dexc", "binance-testnet-creds.json")
)

func tNewBinance(t *testing.T, credsPath string, network dex.Network) *binance {
	b, err := os.ReadFile(credsPath)
	if err != nil {
		t.Fatalf("no credentials file found at %q", credsPath)
	}

	var creds struct {
		Key    string `json:"key"`
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(b, &creds); err != nil {
		t.Fatalf("error decoding credentials: %v", err)
	}

	cex := newBinance(creds.Key, creds.Secret, log, network, true)
	bnc := cex.(*binance)
	return bnc
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
	bnc := tNewBinance(t, testCredsPath, dex.Simnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()

	wg, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	balance, err := bnc.Balance("dcr")
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}
	t.Logf("usdc balance: %v", balance)

	balance, err = bnc.Balance("btc")
	if err != nil {
		t.Fatalf("Balance error: %v", err)
	}
	t.Logf("btc balance: %v", balance)

	wg.Wait()
}

func TestTrade(t *testing.T) {
	bnc := tNewBinance(t, testCredsPath, dex.Simnet)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour*23)
	defer cancel()
	_, err := bnc.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect error: %v", err)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	updates, updaterID := bnc.SubscribeTradeUpdates()
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
	err = bnc.Trade(context.Background(), "eth", "btc", false, 73e4, 1e8, updaterID, tradeID)
	if err != nil {
		t.Fatalf("trade error: %v", err)
	}

	wg.Wait()
}

func TestCancelTrade(t *testing.T) {
	tradeID := "32b22cd2a58645ad8e2b"

	bnc := tNewBinance(t, testCredsPath, dex.Testnet)
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
	bnc := tNewBinance(t, testCredsPath, dex.Testnet)
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

	time.Sleep(2 * time.Second)
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

	err = bnc.UnsubscribeMarket("eth", "btc")
	if err != nil {
		t.Fatalf("error unsubscribing market")
	}

	avg, extrema, filled, err = bnc.VWAP("eth", "btc", true, 2e9)
	if err != nil {
		t.Fatalf("VWAP failed: %v", err)
	}

	t.Logf("avg: %v, extrema: %v, filled: %v", avg, extrema, filled)

	err = bnc.UnsubscribeMarket("eth", "btc")
	if err != nil {
		t.Fatalf("error unsubscribing market")
	}

	avg, extrema, filled, err = bnc.VWAP("eth", "btc", true, 2e9)
	if err == nil {
		t.Fatalf("VWAP did not error after unsubscribing")
	}
}
