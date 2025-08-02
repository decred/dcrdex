package libxc

import (
	"math"
	"reflect"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/mm/libxc/cbtypes"
	"decred.org/dcrdex/dex/calc"
	"github.com/davecgh/go-spew/spew"
)

func TestBuildCoinbaseOrderRequest(t *testing.T) {
	btcUI, _ := asset.UnitInfo(0)
	usdcUI, _ := asset.UnitInfo(966001)
	btcAmt := func(amt float64) uint64 {
		return uint64(math.Round(amt * float64(btcUI.Conventional.ConversionFactor)))
	}
	usdcAmt := func(amt float64) uint64 {
		return uint64(math.Round(amt * float64(usdcUI.Conventional.ConversionFactor)))
	}
	msgRate := func(rate float64) uint64 {
		return calc.MessageRate(rate, btcUI, usdcUI)
	}

	const (
		btcID  = uint32(0)
		usdcID = uint32(966001)
	)

	mkt := &cbtypes.Market{
		ProductID:    "BTC-USDC",
		BaseLotSize:  btcAmt(0.00000001),
		QuoteLotSize: usdcAmt(0.01),
		RateStep:     msgRate(0.01),
		MinBaseQty:   btcAmt(0.00000001),
		MaxBaseQty:   btcAmt(3400),
		MinQuoteQty:  usdcAmt(0.01),
		MaxQuoteQty:  usdcAmt(150_000_000),
	}

	strPtr := func(s string) *string {
		return &s
	}

	tests := []struct {
		name       string
		baseID     uint32
		quoteID    uint32
		sell       bool
		orderType  OrderType
		rate       uint64
		qty        uint64
		quoteQty   uint64
		tradeID    string
		expRequest *cbtypes.OrderRequest
		wantQtyRet uint64
		wantErr    bool
	}{
		{
			name:      "limit buy order 1",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      msgRate(100_000),
			qty:       btcAmt(0.1),
			tradeID:   "test-trade-1",
			expRequest: &cbtypes.OrderRequest{
				ProductID:     "BTC-USDC",
				Side:          "BUY",
				ClientOrderID: "test-trade-1",
				OrderConfig: &cbtypes.LimitOrderConfig{
					Limit: cbtypes.LimitOrderConfigData{
						BaseSize:   "0.10000000",
						LimitPrice: "100000.00",
					},
				},
			},
			wantQtyRet: btcAmt(0.1),
		},
		{
			name:      "limit buy order 2",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      msgRate(100.12345),
			qty:       btcAmt(0.1),
			tradeID:   "test-trade-1",
			expRequest: &cbtypes.OrderRequest{
				ProductID:     "BTC-USDC",
				Side:          "BUY",
				ClientOrderID: "test-trade-1",
				OrderConfig: &cbtypes.LimitOrderConfig{
					Limit: cbtypes.LimitOrderConfigData{
						BaseSize:   "0.10000000",
						LimitPrice: "100.12",
					},
				},
			},
			wantQtyRet: btcAmt(0.1),
		},
		{
			name:      "limit buy qty too low",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      msgRate(100.12345),
			qty:       btcAmt(0),
			tradeID:   "test-trade-2",
			wantErr:   true,
		},
		{
			name:      "limit buy qty too high",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      msgRate(100.12345),
			qty:       btcAmt(3400) + 1,
			tradeID:   "test-trade-2",
			wantErr:   true,
		},
		{
			name:      "limit buy with quote qty",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      msgRate(100_000),
			quoteQty:  usdcAmt(10000),
			tradeID:   "test-trade-1",
			expRequest: &cbtypes.OrderRequest{
				ProductID:     "BTC-USDC",
				Side:          "BUY",
				ClientOrderID: "test-trade-1",
				OrderConfig: &cbtypes.LimitOrderConfig{
					Limit: cbtypes.LimitOrderConfigData{
						BaseSize:   "0.10000000",
						LimitPrice: "100000.00",
					},
				},
			},
			wantQtyRet: calc.QuoteToBase(msgRate(100_000), usdcAmt(10000)),
		},
		{
			name:      "limit buy, quote qty leads to quantity too low",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      msgRate(1e8),
			quoteQty:  usdcAmt(0.01),
			tradeID:   "test-trade-2",
			wantErr:   true,
		},
		{
			name:      "limit buy, quote qty leads to quantity too high",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      msgRate(1),      // Very low rate means large base quantity
			quoteQty:  mkt.MaxQuoteQty, // Use maximum quote quantity
			tradeID:   "test-trade-2",
			wantErr:   true,
		},
		{
			name:      "limit sell order",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      true,
			orderType: OrderTypeLimit,
			rate:      msgRate(100.12345),
			qty:       btcAmt(0.1),
			tradeID:   "test-trade-2",
			expRequest: &cbtypes.OrderRequest{
				ProductID:     "BTC-USDC",
				Side:          "SELL",
				ClientOrderID: "test-trade-2",
				OrderConfig: &cbtypes.LimitOrderConfig{
					Limit: cbtypes.LimitOrderConfigData{
						BaseSize:   "0.10000000",
						LimitPrice: "100.12",
					},
				},
			},
			wantQtyRet: btcAmt(0.1),
		},

		{
			name:      "limit ioc buy order 1",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimitIOC,
			rate:      msgRate(100_000),
			qty:       btcAmt(0.1),
			tradeID:   "test-trade-1",
			expRequest: &cbtypes.OrderRequest{
				ProductID:     "BTC-USDC",
				Side:          "BUY",
				ClientOrderID: "test-trade-1",
				OrderConfig: &cbtypes.LimitIocConfig{
					SorLimitIoc: cbtypes.LimitIocConfigData{
						BaseSize:   "0.10000000",
						LimitPrice: "100000.00",
					},
				},
			},
			wantQtyRet: btcAmt(0.1),
		},
		{
			name:      "limit ioc buy order 2",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimitIOC,
			rate:      msgRate(100.12345),
			qty:       btcAmt(0.1),
			tradeID:   "test-trade-1",
			expRequest: &cbtypes.OrderRequest{
				ProductID:     "BTC-USDC",
				Side:          "BUY",
				ClientOrderID: "test-trade-1",
				OrderConfig: &cbtypes.LimitIocConfig{
					SorLimitIoc: cbtypes.LimitIocConfigData{
						BaseSize:   "0.10000000",
						LimitPrice: "100.12",
					},
				},
			},
			wantQtyRet: btcAmt(0.1),
		},
		{
			name:      "limit ioc buy qty too low",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimitIOC,
			rate:      msgRate(100.12345),
			qty:       btcAmt(0),
			tradeID:   "test-trade-2",
			wantErr:   true,
		},
		{
			name:      "limit ioc buy qty too high",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimitIOC,
			rate:      msgRate(100.12345),
			qty:       btcAmt(3400) + 1,
			tradeID:   "test-trade-2",
			wantErr:   true,
		},
		{
			name:      "limit ioc buy with quote qty",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimitIOC,
			rate:      msgRate(100_000),
			quoteQty:  usdcAmt(10000),
			tradeID:   "test-trade-1",
			expRequest: &cbtypes.OrderRequest{
				ProductID:     "BTC-USDC",
				Side:          "BUY",
				ClientOrderID: "test-trade-1",
				OrderConfig: &cbtypes.LimitIocConfig{
					SorLimitIoc: cbtypes.LimitIocConfigData{
						BaseSize:   "0.10000000",
						LimitPrice: "100000.00",
					},
				},
			},
			wantQtyRet: calc.QuoteToBase(msgRate(100_000), usdcAmt(10000)),
		},
		{
			name:      "limit ioc sell order",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      true,
			orderType: OrderTypeLimitIOC,
			rate:      msgRate(100.12345),
			qty:       btcAmt(0.1),
			tradeID:   "test-trade-2",
			expRequest: &cbtypes.OrderRequest{
				ProductID:     "BTC-USDC",
				Side:          "SELL",
				ClientOrderID: "test-trade-2",
				OrderConfig: &cbtypes.LimitIocConfig{
					SorLimitIoc: cbtypes.LimitIocConfigData{
						BaseSize:   "0.10000000",
						LimitPrice: "100.12",
					},
				},
			},
			wantQtyRet: btcAmt(0.1),
		},

		{
			name:      "market buy with quote qty",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeMarket,
			quoteQty:  usdcAmt(5000),
			tradeID:   "test-trade-3",
			expRequest: &cbtypes.OrderRequest{
				ProductID:     "BTC-USDC",
				Side:          "BUY",
				ClientOrderID: "test-trade-3",
				OrderConfig: &cbtypes.MarketOrderConfig{
					Market: cbtypes.MarketOrderConfigData{
						QuoteSize: strPtr("5000.00"),
					},
				},
			},
			wantQtyRet: usdcAmt(5000), // For market orders with quoteQty, qtyToReturn is the quoteQty
		},
		{
			name:      "market buy with quote qty too low",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeMarket,
			quoteQty:  usdcAmt(0.01) - 1,
			tradeID:   "test-trade-3",
			wantErr:   true,
		},
		{
			name:      "market buy with quote qty too high",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeMarket,
			quoteQty:  usdcAmt(150_000_000) + 1,
			tradeID:   "test-trade-3",
			wantErr:   true,
		},

		{
			name:      "market sell with base qty",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      true,
			orderType: OrderTypeMarket,
			qty:       btcAmt(2),
			tradeID:   "test-trade-4",
			expRequest: &cbtypes.OrderRequest{
				ProductID:     "BTC-USDC",
				Side:          "SELL",
				ClientOrderID: "test-trade-4",
				OrderConfig: &cbtypes.MarketOrderConfig{
					Market: cbtypes.MarketOrderConfigData{
						BaseSize: strPtr("2.00000000"),
					},
				},
			},
			wantQtyRet: btcAmt(2),
		},
		{
			name:      "market sell with base qty too low",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      true,
			orderType: OrderTypeMarket,
			qty:       btcAmt(0.00000001) - 1,
			tradeID:   "test-trade-3",
			wantErr:   true,
		},
		{
			name:      "market sell with base qty too high",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      true,
			orderType: OrderTypeMarket,
			qty:       btcAmt(3400) + 1,
			tradeID:   "test-trade-3",
			wantErr:   true,
		},

		{
			name:      "cannot specify both qty and quote qty",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      msgRate(100_000),
			qty:       btcAmt(0.1),
			quoteQty:  usdcAmt(10000),
			tradeID:   "test-trade-1",
			wantErr:   true,
		},
		{
			name:      "must specify quantity or quote quantity",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeLimit,
			rate:      msgRate(100_000),
			tradeID:   "test-trade-1",
			wantErr:   true,
		},
		{
			name:      "quote quantity cannot be used for sell orders",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      true,
			orderType: OrderTypeMarket,
			quoteQty:  usdcAmt(5000),
			tradeID:   "test-trade-3",
			wantErr:   true,
		},
		{
			name:      "quoteQty MUST be used for market buys",
			baseID:    btcID,
			quoteID:   usdcID,
			sell:      false,
			orderType: OrderTypeMarket,
			qty:       btcAmt(0.1),
			tradeID:   "test-trade-3",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, qtyRet, err := buildOrderRequest(mkt, tt.baseID, tt.quoteID, tt.sell, tt.orderType, tt.rate, tt.qty, tt.quoteQty, tt.tradeID)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if !reflect.DeepEqual(req, tt.expRequest) {
				t.Errorf("expected request %s, got %s", spew.Sdump(tt.expRequest), spew.Sdump(req))
			}
			if qtyRet != tt.wantQtyRet {
				t.Errorf("buildOrderRequest() qtyRet = %v, want %v", qtyRet, tt.wantQtyRet)
			}
		})
	}
}
