// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package libxc

import (
	"math"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"decred.org/dcrdex/client/mm/libxc/bntypes"
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
		{"ETH", "ETH"}:     "eth",
		{"ETH", "MATIC"}:   "weth.polygon",
		{"MATIC", "MATIC"}: "polygon",
		{"USDC", "ETH"}:    "usdc.eth",
		{"USDC", "MATIC"}:  "usdc.polygon",
		{"BTC", "BTC"}:     "btc",
		{"WBTC", "ETH"}:    "wbtc.eth",
	}

	for test, expected := range tests {
		dexSymbol := binanceCoinNetworkToDexSymbol(test[0], test[1])
		if expected != dexSymbol {
			t.Fatalf("expected %s but got %v", expected, dexSymbol)
		}
	}
}

func TestApplyPriceFilter(t *testing.T) {
	tests := []struct {
		name        string
		price       float64
		filter      *bntypes.Filter
		expected    string
		expectError bool
	}{
		{
			name:  "no price filter",
			price: 123.456,
			filter: &bntypes.Filter{
				Type: "OTHER_FILTER",
			},
			expectError: true,
		},
		{
			name:  "round down",
			price: 123.4543,
			filter: &bntypes.Filter{
				Type:     "PRICE_FILTER",
				MinPrice: 0.01,
				MaxPrice: 1000000,
				TickSize: 0.01,
			},
			expected: "123.45",
		},
		{
			name:  "round up",
			price: 123.456789,
			filter: &bntypes.Filter{
				Type:     "PRICE_FILTER",
				MinPrice: 0.1,
				MaxPrice: 1000000,
				TickSize: 0.1,
			},
			expected: "123.5",
		},
		{
			name:  "tick size > 1",
			price: 123.24,
			filter: &bntypes.Filter{
				Type:     "PRICE_FILTER",
				MinPrice: 2,
				MaxPrice: 1000000,
				TickSize: 2,
			},
			expected: "124",
		},
		{
			name:  "exact tick",
			price: 123.400,
			filter: &bntypes.Filter{
				Type:     "PRICE_FILTER",
				MinPrice: 0.1,
				MaxPrice: 1000000,
				TickSize: 0.1,
			},
			expected: "123.4",
		},
		{
			name:  "min price disabled",
			price: 0.001,
			filter: &bntypes.Filter{
				Type:     "PRICE_FILTER",
				MinPrice: 0,
				MaxPrice: 1000000,
				TickSize: 0.001,
			},
			expected: "0.001",
		},
		{
			name:  "max price disabled",
			price: 2000000,
			filter: &bntypes.Filter{
				Type:     "PRICE_FILTER",
				MinPrice: 0.1,
				MaxPrice: 0,
				TickSize: 0.1,
			},
			expected: "2000000.0",
		},
		{
			name:  "below min price",
			price: 0.009,
			filter: &bntypes.Filter{
				Type:     "PRICE_FILTER",
				MinPrice: 0.01,
				MaxPrice: 1000000,
				TickSize: 0.001,
			},
			expectError: true,
		},
		{
			name:  "above max price",
			price: 1000001,
			filter: &bntypes.Filter{
				Type:     "PRICE_FILTER",
				MinPrice: 0.1,
				MaxPrice: 1000000,
				TickSize: 0.1,
			},
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := applyPriceFilter(test.price, []*bntypes.Filter{test.filter})
			if test.expectError {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != test.expected {
				t.Errorf("applyPriceFilter(%v, %+v) = %v, want %v",
					test.price, test.filter, result, test.expected)
			}
		})
	}
}

func TestApplyLotSizeFilter(t *testing.T) {
	tests := []struct {
		name        string
		qty         float64
		filter      *bntypes.Filter
		expected    string
		expectError bool
	}{
		{
			name: "no lot size filter",
			qty:  1.23,
			filter: &bntypes.Filter{
				Type: "OTHER_FILTER",
			},
			expectError: true,
		},
		{
			name: "basic lot size",
			qty:  1.23,
			filter: &bntypes.Filter{
				Type:     "LOT_SIZE",
				MinQty:   0.1,
				MaxQty:   1000,
				StepSize: 0.01,
			},
			expected: "1.23",
		},
		{
			name: "round to step size",
			qty:  1.234567,
			filter: &bntypes.Filter{
				Type:     "LOT_SIZE",
				MinQty:   0.1,
				MaxQty:   1000,
				StepSize: 0.01,
			},
			expected: "1.23",
		},
		{
			name: "zero step size",
			qty:  1.23,
			filter: &bntypes.Filter{
				Type:     "LOT_SIZE",
				MinQty:   0.1,
				MaxQty:   1000,
				StepSize: 0,
			},
			expectError: true,
		},
		{
			name: "below min quantity",
			qty:  0.05,
			filter: &bntypes.Filter{
				Type:     "LOT_SIZE",
				MinQty:   0.1,
				MaxQty:   1000,
				StepSize: 0.01,
			},
			expectError: true,
		},
		{
			name: "above max quantity",
			qty:  1001,
			filter: &bntypes.Filter{
				Type:     "LOT_SIZE",
				MinQty:   0.1,
				MaxQty:   1000,
				StepSize: 0.01,
			},
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := applyLotSizeFilter(test.qty, []*bntypes.Filter{test.filter})
			if test.expectError {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != test.expected {
				t.Errorf("applyLotSizeFilter(%v, %+v) = %v, want %v",
					test.qty, test.filter, result, test.expected)
			}
		})
	}
}

func TestSteppedAmount(t *testing.T) {
	tests := []struct {
		name        string
		amt         float64
		integerMult float64
		expected    string
	}{
		{
			name:        "zero amount",
			amt:         0,
			integerMult: 0.01,
			expected:    "0.01",
		},
		{
			name:        "small amount",
			amt:         0.001,
			integerMult: 0.01,
			expected:    "0.01",
		},
		{
			name:        "exact multiple",
			amt:         0.05,
			integerMult: 0.01,
			expected:    "0.05",
		},
		{
			name:        "round down",
			amt:         0.054,
			integerMult: 0.01,
			expected:    "0.05",
		},
		{
			name:        "round up",
			amt:         0.056,
			integerMult: 0.01,
			expected:    "0.06",
		},
		{
			name:        "integer multiple > 1",
			amt:         123.24,
			integerMult: 2,
			expected:    "124",
		},
		{
			name:        "small decimal",
			amt:         0.000123,
			integerMult: 0.0001,
			expected:    "0.0001",
		},
		{
			name:        "large number",
			amt:         1234567.89,
			integerMult: 10,
			expected:    "1234570",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := steppedAmount(test.amt, test.integerMult)
			if result != test.expected {
				t.Errorf("steppedAmount(%v, %v) = %v, want %v",
					test.amt, test.integerMult, result, test.expected)
			}
		})
	}
}

func FuzzFloatToScaledUintRoundTrip(f *testing.F) {
	// Add some initial seed values
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 100; i++ {
		// Generate random values in different ranges
		var val float64
		switch i % 5 {
		case 0:
			// Very small decimals
			val = rnd.Float64() * 0.0001
		case 1:
			// Numbers between 0 and 1
			val = rnd.Float64()
		case 2:
			// Numbers between 1 and 1000
			val = rnd.Float64() * 1000
		case 3:
			// Large numbers
			val = rnd.Float64() * 1000000
		case 4:
			// Numbers with many decimal places
			val = rnd.Float64() * math.Pow10(rnd.Intn(10))
		}
		f.Add(val)
	}

	f.Fuzz(func(t *testing.T, orig float64) {
		// Skip NaN and Inf values
		if math.IsNaN(orig) || math.IsInf(orig, 0) {
			t.Skip()
		}

		// Get absolute value since we only handle positive numbers
		orig = math.Abs(orig)

		// Convert to scaled uint
		scaled, decimals := floatToScaledUint(orig)

		// Convert back to string
		result := scaledUintToString(scaled, decimals)

		// Format original float with same precision
		expected := strconv.FormatFloat(orig, 'f', -1, 64)

		// Compare the string representations
		if result != expected {
			t.Errorf("Round trip failed: orig=%v scaled=%v decimals=%v result=%v expected=%v",
				orig, scaled, decimals, result, expected)
		}
	})
}
