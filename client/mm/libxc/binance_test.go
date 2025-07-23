// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

import (
	"math"
	"math/big"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"decred.org/dcrdex/client/asset"
	_ "decred.org/dcrdex/client/asset/importall"
	"decred.org/dcrdex/client/mm/libxc/bntypes"
	"decred.org/dcrdex/dex/calc"
	"github.com/davecgh/go-spew/spew"
)

func TestSubscribeTradeUpdates(t *testing.T) {
	bn := &binance{
		tradeUpdaters: make(map[int]chan *Trade),
	}
	_, unsub0, _ := bn.SubscribeTradeUpdates()
	_, _, id1 := bn.SubscribeTradeUpdates()
	unsub0()
	_, _, id2 := bn.SubscribeTradeUpdates()
	if len(bn.tradeUpdaters) != 2 {
		t.Fatalf("wrong number of updaters. wanted 2, got %d", len(bn.tradeUpdaters))
	}
	if id1 == id2 {
		t.Fatalf("ids should be unique. got %d twice", id1)
	}
	if _, found := bn.tradeUpdaters[id1]; !found {
		t.Fatalf("id1 not found")
	}
	if _, found := bn.tradeUpdaters[id2]; !found {
		t.Fatalf("id2 not found")
	}
}

func TestBinanceToDexSymbol(t *testing.T) {
	tests := map[[2]string]string{
		{"ETH", "ETH"}:    "eth",
		{"ETH", "MATIC"}:  "weth.polygon",
		{"USDC", "ETH"}:   "usdc.eth",
		{"USDC", "MATIC"}: "usdc.polygon",
		{"BTC", "BTC"}:    "btc",
		{"WBTC", "ETH"}:   "wbtc.eth",
		{"POL", "MATIC"}:  "polygon",
	}

	for test, expected := range tests {
		dexSymbol := binanceCoinNetworkToDexSymbol(test[0], test[1])
		if expected != dexSymbol {
			t.Fatalf("expected %s but got %v", expected, dexSymbol)
		}
	}
}

func TestBncAssetCfg(t *testing.T) {
	tests := map[uint32]*bncAssetConfig{
		0: {
			assetID:          0,
			symbol:           "btc",
			coin:             "BTC",
			chain:            "BTC",
			conversionFactor: 1e8,
		},
		2: {
			assetID:          2,
			symbol:           "ltc",
			coin:             "LTC",
			chain:            "LTC",
			conversionFactor: 1e8,
		},
		3: {
			assetID:          3,
			symbol:           "doge",
			coin:             "DOGE",
			chain:            "DOGE",
			conversionFactor: 1e8,
		},
		5: {
			assetID:          5,
			symbol:           "dash",
			coin:             "DASH",
			chain:            "DASH",
			conversionFactor: 1e8,
		},
		60: {
			assetID:          60,
			symbol:           "eth",
			coin:             "ETH",
			chain:            "ETH",
			conversionFactor: 1e9,
		},
		42: {
			assetID:          42,
			symbol:           "dcr",
			coin:             "DCR",
			chain:            "DCR",
			conversionFactor: 1e8,
		},
		966001: {
			assetID:          966001,
			symbol:           "usdc.polygon",
			coin:             "USDC",
			chain:            "MATIC",
			conversionFactor: 1e6,
		},
		966002: {
			assetID:          966002,
			symbol:           "weth.polygon",
			coin:             "ETH",
			chain:            "MATIC",
			conversionFactor: 1e9,
		},
		966: {
			assetID:          966,
			symbol:           "polygon",
			coin:             "POL",
			chain:            "MATIC",
			conversionFactor: 1e9,
		},
	}

	for test, expected := range tests {
		cfg, err := bncAssetCfg(test)
		if err != nil {
			t.Fatalf("error getting asset config: %v", err)
		}
		if cfg.ui == nil {
			t.Fatalf("ui is nil for %v", test)
		}
		cfg.ui = nil
		if !reflect.DeepEqual(expected, cfg) {
			t.Fatalf("expected %v but got %v", expected, cfg)
		}
	}
}

func TestParseFilters(t *testing.T) {
	baseID := uint32(60)
	quoteID := uint32(0)
	bui, _ := asset.UnitInfo(baseID)
	qui, _ := asset.UnitInfo(quoteID)

	type test struct {
		name    string
		filters []*bntypes.Filter

		expMarket *bntypes.Market
		expError  bool
	}

	tests := []test{
		{
			name:     "no filters",
			expError: true,
		},
		{
			name: "price, lot size, notional",
			filters: []*bntypes.Filter{
				{
					Type:     "PRICE_FILTER",
					MinPrice: 0.00001000,
					MaxPrice: 922327.00000000,
					TickSize: 0.00001000,
				},
				{
					Type:     "LOT_SIZE",
					MinQty:   0.00010000,
					MaxQty:   100000.00000000,
					StepSize: 0.00010000,
				},
				{
					Type:             "NOTIONAL",
					MinNotional:      0.00010000,
					MaxNotional:      9000000.00000000,
					ApplyMinToMarket: true,
					ApplyMaxToMarket: true,
				},
			},
			expMarket: &bntypes.Market{
				Symbol:                   "ETHBTC",
				MinPrice:                 calc.MessageRate(0.00001000, bui, qui),
				MaxPrice:                 calc.MessageRate(922327, bui, qui),
				RateStep:                 calc.MessageRate(0.00001, bui, qui),
				MinQty:                   uint64(math.Round(0.00010000 * float64(bui.Conventional.ConversionFactor))),
				MaxQty:                   uint64(math.Round(100000.00000000 * float64(bui.Conventional.ConversionFactor))),
				LotSize:                  uint64(math.Round(0.00010000 * float64(bui.Conventional.ConversionFactor))),
				MinNotional:              uint64(math.Round(0.00010000 * float64(qui.Conventional.ConversionFactor))),
				MaxNotional:              uint64(math.Round(9000000.00000000 * float64(qui.Conventional.ConversionFactor))),
				ApplyMinNotionalToMarket: true,
				ApplyMaxNotionalToMarket: true,
			},
		},
		{
			name: "price, lot size, min notional",
			filters: []*bntypes.Filter{
				{
					Type:     "PRICE_FILTER",
					MinPrice: 0.00001000,
					MaxPrice: 922327.00000000,
					TickSize: 0.00001000,
				},
				{
					Type:     "LOT_SIZE",
					MinQty:   0.00010000,
					MaxQty:   100000.00000000,
					StepSize: 0.00010000,
				},
				{
					Type:          "MIN_NOTIONAL",
					MinNotional:   0.00010000,
					ApplyToMarket: true,
				},
			},
			expMarket: &bntypes.Market{
				Symbol:                   "ETHBTC",
				MinPrice:                 calc.MessageRate(0.00001000, bui, qui),
				MaxPrice:                 calc.MessageRate(922327, bui, qui),
				RateStep:                 calc.MessageRate(0.00001, bui, qui),
				MinQty:                   uint64(math.Round(0.00010000 * float64(bui.Conventional.ConversionFactor))),
				MaxQty:                   uint64(math.Round(100000.00000000 * float64(bui.Conventional.ConversionFactor))),
				LotSize:                  uint64(math.Round(0.00010000 * float64(bui.Conventional.ConversionFactor))),
				MinNotional:              uint64(math.Round(0.00010000 * float64(qui.Conventional.ConversionFactor))),
				MaxNotional:              math.MaxUint64,
				ApplyMinNotionalToMarket: true,
				ApplyMaxNotionalToMarket: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mkt := &bntypes.Market{
				Symbol:  "ETHBTC",
				Filters: tt.filters,
			}
			gotMarket, err := parseMarketFilters(mkt, bui, qui)
			if (err != nil) != tt.expError {
				t.Errorf("parseMarketFilters() error = %v, expError %v", err, tt.expError)
				return
			}
			if tt.expError {
				return
			}
			gotMarket.Filters = nil
			if !reflect.DeepEqual(gotMarket, tt.expMarket) {
				t.Errorf("parseMarketFilters() = %s, exp %s", spew.Sdump(gotMarket), spew.Sdump(tt.expMarket))
			}
		})
	}
}

func TestBuildTradeRequest(t *testing.T) {
	bui, _ := asset.UnitInfo(60)
	qui, _ := asset.UnitInfo(0)
	baseCfg := &bncAssetConfig{
		assetID:          60,
		symbol:           "eth",
		coin:             "ETH",
		chain:            "ETH",
		conversionFactor: 1e9,
		ui:               &bui,
	}
	quoteCfg := &bncAssetConfig{
		assetID:          0,
		symbol:           "btc",
		coin:             "BTC",
		chain:            "BTC",
		conversionFactor: 1e8,
		ui:               &qui,
	}

	market := &bntypes.Market{
		Symbol:                   "ETHBTC",
		MinPrice:                 calc.MessageRate(0.00001000, bui, qui),
		MaxPrice:                 calc.MessageRate(922327, bui, qui),
		RateStep:                 calc.MessageRate(0.00001, bui, qui),
		MinQty:                   uint64(math.Round(0.001 * float64(bui.Conventional.ConversionFactor))),
		MaxQty:                   uint64(math.Round(100000 * float64(bui.Conventional.ConversionFactor))),
		LotSize:                  uint64(math.Round(0.0001 * float64(bui.Conventional.ConversionFactor))),
		MinNotional:              uint64(math.Round(0.0001 * float64(qui.Conventional.ConversionFactor))),
		MaxNotional:              uint64(math.Round(9000000.00000000 * float64(qui.Conventional.ConversionFactor))),
		ApplyMinNotionalToMarket: true,
		ApplyMaxNotionalToMarket: true,
	}
	tradeID := "test123"

	qtysToRate := func(baseQty, quoteQty uint64) uint64 {
		bigBase := big.NewInt(int64(baseQty))
		bigQuote := big.NewInt(int64(quoteQty))
		bigRateConversionFactor := big.NewInt(1e8)
		bigQuote.Mul(bigQuote, bigRateConversionFactor)
		bigQuote.Div(bigQuote, bigBase)
		return bigQuote.Uint64()
	}

	tests := []struct {
		name       string
		sell       bool
		orderType  OrderType
		rate       uint64
		qty        uint64
		quoteQty   uint64
		avgPrice   uint64
		wantErr    string
		wantVals   url.Values
		wantQtyRet uint64
	}{
		// =================== limit buy ===========================
		{
			name:      "limit buy",
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      10000,
			qty:       5e9,
			wantVals: url.Values{
				"symbol":           []string{"ETHBTC"},
				"side":             []string{"BUY"},
				"type":             []string{"LIMIT"},
				"timeInForce":      []string{"GTC"},
				"newClientOrderId": []string{"test123"},
				"quantity":         []string{"5.0000"},
				"price":            []string{"0.00100"},
			},
			wantQtyRet: 5e9,
		},
		{
			name:      "limit buy, rate too low",
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      market.MinPrice - 1,
			qty:       5e9,
			wantErr:   "rate",
		},
		{
			name:      "limit buy, rate too high",
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      market.MaxPrice + 1,
			qty:       5e9,
			wantErr:   "rate",
		},
		{
			name:      "limit buy, quantity too low",
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      1000,
			qty:       market.MinQty - 1,
			wantErr:   "quantity",
		},
		{
			name:      "limit buy, quantity too high",
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      1000,
			qty:       market.MaxQty + 1,
			wantErr:   "quantity",
		},
		{
			name:      "limit buy, notional too low",
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      qtysToRate(5e9, market.MinNotional-1),
			qty:       5e9,
			wantErr:   "notional",
		},
		{
			name:      "limit buy, notional too high",
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      qtysToRate(market.MaxQty, market.MaxNotional+1e6),
			qty:       market.MaxQty,
			wantErr:   "notional",
		},
		// =================== limit buy with quote qty ===========================
		{
			name:      "limit buy with quote qty",
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      10000,
			quoteQty:  500000,
			wantVals: url.Values{
				"symbol":           []string{"ETHBTC"},
				"side":             []string{"BUY"},
				"type":             []string{"LIMIT"},
				"timeInForce":      []string{"GTC"},
				"newClientOrderId": []string{"test123"},
				"quantity":         []string{"5.0000"},
				"price":            []string{"0.00100"},
			},
			wantQtyRet: steppedQty(calc.QuoteToBase(10000, 500000), market.LotSize),
		},
		{
			name:      "limit buy, quote qty leads to quantity too low",
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      1e8,
			quoteQty:  calc.BaseToQuote(1e8, market.LotSize),
			wantErr:   "quantity",
		},
		{
			name:      "limit buy, quote qty leads to quantity too high",
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      market.MinPrice,    // Use minimum valid rate
			quoteQty:  market.MaxNotional, // Use maximum notional as quote quantity
			wantErr:   "quantity",
		},

		// =================== limit sell ===========================
		{
			name:      "limit sell",
			sell:      true,
			orderType: OrderTypeLimit,
			rate:      50000,
			qty:       5e9,
			wantVals: url.Values{
				"symbol":           []string{"ETHBTC"},
				"side":             []string{"SELL"},
				"type":             []string{"LIMIT"},
				"timeInForce":      []string{"GTC"},
				"newClientOrderId": []string{"test123"},
				"quantity":         []string{"5.0000"},
				"price":            []string{"0.00500"},
			},
			wantQtyRet: 5e9,
		},
		{
			name:      "limit sell, rate too low",
			sell:      true,
			orderType: OrderTypeLimit,
			rate:      market.MinPrice - 1,
			qty:       5e9,
			wantErr:   "rate",
		},
		{
			name:      "limit sell, rate too high",
			sell:      true,
			orderType: OrderTypeLimit,
			rate:      market.MaxPrice + 1,
			qty:       5e9,
			wantErr:   "rate",
		},
		{
			name:      "limit sell, quantity too low",
			sell:      true,
			orderType: OrderTypeLimit,
			rate:      1000,
			qty:       market.MinQty - 1,
			wantErr:   "quantity",
		},
		{
			name:      "limit sell, quantity too high",
			sell:      true,
			orderType: OrderTypeLimit,
			rate:      1000,
			qty:       market.MaxQty + 1,
			wantErr:   "quantity",
		},
		{
			name:      "limit sell, notional too low",
			sell:      true,
			orderType: OrderTypeLimit,
			rate:      qtysToRate(5e9, market.MinNotional-1),
			qty:       5e9,
			wantErr:   "notional",
		},
		{
			name:      "limit sell, notional too high",
			sell:      true,
			orderType: OrderTypeLimit,
			rate:      qtysToRate(market.MaxQty, market.MaxNotional+1e6),
			qty:       market.MaxQty,
			wantErr:   "notional",
		},
		// =================== limit ioc buy ===========================
		{
			name:      "limit ioc buy",
			sell:      false,
			orderType: OrderTypeLimitIOC,
			rate:      10000,
			qty:       5e9,
			wantVals: url.Values{
				"symbol":           []string{"ETHBTC"},
				"side":             []string{"BUY"},
				"type":             []string{"LIMIT"},
				"timeInForce":      []string{"IOC"},
				"newClientOrderId": []string{"test123"},
				"quantity":         []string{"5.0000"},
				"price":            []string{"0.00100"},
			},
			wantQtyRet: 5e9,
		},
		{
			name:      "limit ioc buy, rate too low",
			sell:      false,
			orderType: OrderTypeLimitIOC,
			rate:      market.MinPrice - 1,
			qty:       5e9,
			wantErr:   "rate",
		},
		{
			name:      "limit ioc buy, rate too high",
			sell:      false,
			orderType: OrderTypeLimitIOC,
			rate:      market.MaxPrice + 1,
			qty:       5e9,
			wantErr:   "rate",
		},
		{
			name:      "limit ioc buy, quantity too low",
			sell:      false,
			orderType: OrderTypeLimitIOC,
			rate:      1000,
			qty:       market.MinQty - 1,
			wantErr:   "quantity",
		},
		{
			name:      "limit ioc buy, quantity too high",
			sell:      false,
			orderType: OrderTypeLimitIOC,
			rate:      1000,
			qty:       market.MaxQty + 1,
			wantErr:   "quantity",
		},
		{
			name:      "limit ioc buy, notional too low",
			sell:      false,
			orderType: OrderTypeLimitIOC,
			rate:      qtysToRate(5e9, market.MinNotional-1),
			qty:       5e9,
			wantErr:   "notional",
		},
		{
			name:      "limit ioc buy, notional too high",
			sell:      false,
			orderType: OrderTypeLimitIOC,
			rate:      qtysToRate(market.MaxQty, market.MaxNotional+1e6),
			qty:       market.MaxQty,
			wantErr:   "notional",
		},
		// =================== limit ioc buy with quote qty ===========================
		{
			name:      "limit ioc buy with quote qty",
			sell:      false,
			orderType: OrderTypeLimitIOC,
			rate:      10000,
			quoteQty:  500000,
			wantVals: url.Values{
				"symbol":           []string{"ETHBTC"},
				"side":             []string{"BUY"},
				"type":             []string{"LIMIT"},
				"timeInForce":      []string{"IOC"},
				"newClientOrderId": []string{"test123"},
				"quantity":         []string{"5.0000"},
				"price":            []string{"0.00100"},
			},
			wantQtyRet: 5e9,
		},

		// =================== limit ioc sell ===========================
		{
			name:      "limit ioc sell",
			sell:      true,
			orderType: OrderTypeLimitIOC,
			rate:      50000,
			qty:       5e9,
			wantVals: url.Values{
				"symbol":           []string{"ETHBTC"},
				"side":             []string{"SELL"},
				"type":             []string{"LIMIT"},
				"timeInForce":      []string{"IOC"},
				"newClientOrderId": []string{"test123"},
				"quantity":         []string{"5.0000"},
				"price":            []string{"0.00500"},
			},
			wantQtyRet: 5e9,
		},
		{
			name:      "limit ioc sell, rate too low",
			sell:      true,
			orderType: OrderTypeLimitIOC,
			rate:      market.MinPrice - 1,
			qty:       5e9,
			wantErr:   "rate",
		},
		{
			name:      "limit ioc sell, rate too high",
			sell:      true,
			orderType: OrderTypeLimitIOC,
			rate:      market.MaxPrice + 1,
			qty:       5e9,
			wantErr:   "rate",
		},
		{
			name:      "limit ioc sell, quantity too low",
			sell:      true,
			orderType: OrderTypeLimitIOC,
			rate:      1000,
			qty:       market.MinQty - 1,
			wantErr:   "quantity",
		},
		{
			name:      "limit ioc sell, quantity too high",
			sell:      true,
			orderType: OrderTypeLimitIOC,
			rate:      1000,
			qty:       market.MaxQty + 1,
			wantErr:   "quantity",
		},
		{
			name:      "limit ioc sell, notional too low",
			sell:      true,
			orderType: OrderTypeLimitIOC,
			rate:      qtysToRate(5e9, market.MinNotional-1),
			qty:       5e9,
			wantErr:   "notional",
		},
		{
			name:      "limit ioc sell, notional too high",
			sell:      true,
			orderType: OrderTypeLimitIOC,
			rate:      qtysToRate(market.MaxQty, market.MaxNotional+1e6),
			qty:       market.MaxQty,
			wantErr:   "notional",
		},

		// =================== market buy with quote qty ===========================
		{
			name:      "market buy with quote qty",
			sell:      false,
			orderType: OrderTypeMarket,
			quoteQty:  5e7,
			wantVals: url.Values{
				"symbol":           []string{"ETHBTC"},
				"side":             []string{"BUY"},
				"type":             []string{"MARKET"},
				"newClientOrderId": []string{"test123"},
				"quoteOrderQty":    []string{"0.50000000"},
			},
			wantQtyRet: 5e7,
		},
		{
			name:      "market buy with quote qty, notional too low",
			sell:      false,
			orderType: OrderTypeMarket,
			quoteQty:  market.MinNotional - 1,
			wantErr:   "notional",
		},
		{
			name:      "market buy with quote qty, notional too high",
			sell:      false,
			orderType: OrderTypeMarket,
			quoteQty:  market.MaxNotional + 1,
			wantErr:   "notional",
		},

		// =================== market sell with base qty ===========================
		{
			name:      "market sell with base qty",
			sell:      true,
			orderType: OrderTypeMarket,
			qty:       5e9,
			avgPrice:  market.MinQty,
			wantVals: url.Values{
				"symbol":           []string{"ETHBTC"},
				"side":             []string{"SELL"},
				"type":             []string{"MARKET"},
				"newClientOrderId": []string{"test123"},
				"quantity":         []string{"5.0000"},
			},
			wantQtyRet: 5e9,
		},
		{
			name:      "market sell with base qty, min qty",
			sell:      true,
			orderType: OrderTypeMarket,
			qty:       market.MinQty,
			avgPrice:  1e8,
			wantVals: url.Values{
				"symbol":           []string{"ETHBTC"},
				"side":             []string{"SELL"},
				"type":             []string{"MARKET"},
				"newClientOrderId": []string{"test123"},
				"quantity":         []string{"0.0010"},
			},
			wantQtyRet: market.MinQty,
		},
		{
			name:      "market sell with base qty, quantity too low",
			sell:      true,
			orderType: OrderTypeMarket,
			qty:       market.MinQty - 1,
			wantErr:   "quantity",
		},
		{
			name:      "market sell with base qty, quantity too high",
			sell:      true,
			orderType: OrderTypeMarket,
			qty:       market.MaxQty + 1,
			wantErr:   "quantity",
		},
		{
			name:      "market sell with base qty, notional too low",
			sell:      true,
			orderType: OrderTypeMarket,
			avgPrice:  qtysToRate(5e9, market.MinNotional-1),
			qty:       5e9,
			wantErr:   "notional",
		},
		{
			name:      "market sell with base qty, notional too high",
			sell:      true,
			orderType: OrderTypeMarket,
			avgPrice:  qtysToRate(market.MaxQty, market.MaxNotional+1e6),
			qty:       market.MaxQty,
			wantErr:   "notional",
		},

		// =================== mutual exclusivity ===========================
		{
			name:      "cannot specify both qty and quote qty",
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      10000,
			qty:       5e9,
			quoteQty:  500000,
			wantErr:   "cannot specify both",
		},
		{
			name:      "quote quantity cannot be used for sell orders",
			sell:      true,
			orderType: OrderTypeMarket,
			quoteQty:  5e7,
			wantErr:   "quote quantity cannot be used for sell orders",
		},
		{
			name:      "quoteQty MUST be used for market buys",
			sell:      false,
			orderType: OrderTypeMarket,
			qty:       5e9,
			wantErr:   "quoteQty MUST be used for market buys",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vals, qtyRet, err := buildTradeRequest(baseCfg, quoteCfg, market, tt.avgPrice, tt.sell, tt.orderType, tt.rate, tt.qty, tt.quoteQty, tradeID)
			if (err != nil) != (tt.wantErr != "") {
				t.Errorf("buildTradeRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr != "" && err != nil && !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("buildTradeRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr != "" {
				return
			}
			if len(vals) != len(tt.wantVals) {
				t.Errorf("buildTradeRequest() got %d values, want %d", len(vals), len(tt.wantVals))
				return
			}
			for k, want := range tt.wantVals {
				got := vals[k]
				if len(got) != 1 || got[0] != want[0] {
					t.Errorf("buildTradeRequest() key %q = %v, want %v", k, got, want)
				}
			}
			if qtyRet != tt.wantQtyRet {
				t.Errorf("buildTradeRequest() qtyRet = %v, want %v", qtyRet, tt.wantQtyRet)
			}
		})
	}
}
