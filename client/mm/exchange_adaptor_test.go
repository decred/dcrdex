package mm

import (
	"context"
	"encoding/hex"
	"fmt"
	"reflect"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

func TestExchangeAdaptorMaxSell(t *testing.T) {
	tCore := newTCore()
	tCore.isAccountLocker[60] = true
	dcrBtcID := fmt.Sprintf("%s-%d-%d", "host1", 42, 0)
	dcrEthID := fmt.Sprintf("%s-%d-%d", "host1", 42, 60)

	// Whatever is returned from PreOrder is returned from this function.
	// What we need to test is what is passed to PreOrder.
	orderEstimate := &core.OrderEstimate{
		Swap: &asset.PreSwap{
			Estimate: &asset.SwapEstimate{
				Lots:               5,
				Value:              5e8,
				MaxFees:            1600,
				RealisticWorstCase: 12010,
				RealisticBestCase:  6008,
			},
		},
		Redeem: &asset.PreRedeem{
			Estimate: &asset.RedeemEstimate{
				RealisticBestCase:  2800,
				RealisticWorstCase: 6500,
			},
		},
	}
	tCore.orderEstimate = orderEstimate

	expectedResult := &core.MaxOrderEstimate{
		Swap: &asset.SwapEstimate{
			Lots:               5,
			Value:              5e8,
			MaxFees:            1600,
			RealisticWorstCase: 12010,
			RealisticBestCase:  6008,
		},
		Redeem: &asset.RedeemEstimate{
			RealisticBestCase:  2800,
			RealisticWorstCase: 6500,
		},
	}

	tests := []struct {
		name          string
		assetBalances map[uint32]uint64
		market        *core.Market
		swapFees      uint64
		redeemFees    uint64
		refundFees    uint64

		expectPreOrderParam *core.TradeForm
		wantErr             bool
	}{
		{
			name: "ok",
			assetBalances: map[uint32]uint64{
				0:  5e6,
				42: 5e6,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  42,
				QuoteID: 0,
			},
			expectPreOrderParam: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    true,
				Qty:     4 * 1e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
		},
		{
			name: "1 lot",
			assetBalances: map[uint32]uint64{
				42: 1e6 + 1000,
				0:  1000,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  42,
				QuoteID: 0,
			},
			expectPreOrderParam: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    true,
				Qty:     1e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
		},
		{
			name: "not enough for 1 swap",
			assetBalances: map[uint32]uint64{
				0:  1e6 + 999,
				42: 1000,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  42,
				QuoteID: 0,
			},
			swapFees:   1000,
			redeemFees: 1000,
			wantErr:    true,
		},
		{
			name: "not enough for 1 lot of redeem fees",
			assetBalances: map[uint32]uint64{
				42: 1e6 + 1000,
				60: 999,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  42,
				QuoteID: 60,
			},
			swapFees:   1000,
			redeemFees: 1000,
			wantErr:    true,
		},
		{
			name: "redeem fees don't matter if not account locker",
			assetBalances: map[uint32]uint64{
				42: 1e6 + 1000,
				0:  999,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  42,
				QuoteID: 0,
			},
			swapFees:   1000,
			redeemFees: 1000,
			expectPreOrderParam: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    true,
				Qty:     1e6,
			},
		},
		{
			name: "2 lots with refund fees, not account locker",
			assetBalances: map[uint32]uint64{
				42: 2e6 + 2000,
				0:  1000,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  42,
				QuoteID: 0,
			},
			expectPreOrderParam: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    true,
				Qty:     2e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			refundFees: 1000,
		},
		{
			name: "1 lot with refund fees, account locker",
			assetBalances: map[uint32]uint64{
				60: 1000,
				42: 2e6 + 2000,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  42,
				QuoteID: 60,
			},
			expectPreOrderParam: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   60,
				Sell:    true,
				Qty:     1e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			refundFees: 1000,
		},
	}

	for _, test := range tests {
		tCore.setAssetBalances(test.assetBalances)
		tCore.market = test.market
		tCore.sellSwapFees = test.swapFees
		tCore.sellRedeemFees = test.redeemFees
		tCore.sellRefundFees = test.refundFees
		tCore.isAccountLocker[60] = true

		botID := dcrBtcID
		if test.market.QuoteID == 60 {
			botID = dcrEthID
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		adaptor := unifiedExchangeAdaptorForBot(botID, test.assetBalances, nil, tCore, nil, tLogger)
		adaptor.run(ctx)
		res, err := adaptor.MaxSell("host1", test.market.BaseID, test.market.QuoteID)
		if test.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error but did not get", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		if !reflect.DeepEqual(tCore.preOrderParam, test.expectPreOrderParam) {
			t.Fatalf("%s: expected pre order param %+v != actual %+v", test.name, test.expectPreOrderParam, tCore.preOrderParam)
		}

		if !reflect.DeepEqual(res, expectedResult) {
			t.Fatalf("%s: expected max sell result %+v != actual %+v", test.name, expectedResult, res)
		}
	}
}

func TestExchangeAdaptorMaxBuy(t *testing.T) {
	tCore := newTCore()

	tCore.isAccountLocker[60] = true
	dcrBtcID := fmt.Sprintf("%s-%d-%d", "host1", 42, 0)
	ethBtcID := fmt.Sprintf("%s-%d-%d", "host1", 60, 0)

	// Whatever is returned from PreOrder is returned from this function.
	// What we need to test is what is passed to PreOrder.
	orderEstimate := &core.OrderEstimate{
		Swap: &asset.PreSwap{
			Estimate: &asset.SwapEstimate{
				Lots:               5,
				Value:              5e8,
				MaxFees:            1600,
				RealisticWorstCase: 12010,
				RealisticBestCase:  6008,
			},
		},
		Redeem: &asset.PreRedeem{
			Estimate: &asset.RedeemEstimate{
				RealisticBestCase:  2800,
				RealisticWorstCase: 6500,
			},
		},
	}
	tCore.orderEstimate = orderEstimate

	expectedResult := &core.MaxOrderEstimate{
		Swap: &asset.SwapEstimate{
			Lots:               5,
			Value:              5e8,
			MaxFees:            1600,
			RealisticWorstCase: 12010,
			RealisticBestCase:  6008,
		},
		Redeem: &asset.RedeemEstimate{
			RealisticBestCase:  2800,
			RealisticWorstCase: 6500,
		},
	}

	tests := []struct {
		name        string
		dexBalances map[uint32]uint64
		market      *core.Market
		rate        uint64
		swapFees    uint64
		redeemFees  uint64
		refundFees  uint64

		expectPreOrderParam *core.TradeForm
		wantErr             bool
	}{
		{
			name: "ok",
			rate: 5e7,
			dexBalances: map[uint32]uint64{
				0:  5e6,
				42: 5e6,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  42,
				QuoteID: 0,
			},
			expectPreOrderParam: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    false,
				Rate:    5e7,
				Qty:     9 * 1e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
		},
		{
			name: "1 lot",
			rate: 5e7,
			dexBalances: map[uint32]uint64{
				42: 1000,
				0:  calc.BaseToQuote(5e7, 1e6) + 1000,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  42,
				QuoteID: 0,
			},
			expectPreOrderParam: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    false,
				Qty:     1e6,
				Rate:    5e7,
			},
			swapFees:   1000,
			redeemFees: 1000,
		},
		{
			name: "not enough for 1 swap",
			rate: 5e7,
			dexBalances: map[uint32]uint64{
				0:  1000,
				42: calc.BaseToQuote(5e7, 1e6) + 999,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  42,
				QuoteID: 0,
			},
			swapFees:   1000,
			redeemFees: 1000,
			wantErr:    true,
		},
		{
			name: "not enough for 1 lot of redeem fees",
			rate: 5e7,
			dexBalances: map[uint32]uint64{
				0:  calc.BaseToQuote(5e7, 1e6) + 1000,
				60: 999,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  60,
				QuoteID: 0,
			},
			swapFees:   1000,
			redeemFees: 1000,
			wantErr:    true,
		},
		{
			name: "only account locker affected by redeem fees",
			rate: 5e7,
			dexBalances: map[uint32]uint64{
				0:  calc.BaseToQuote(5e7, 1e6) + 1000,
				42: 999,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  42,
				QuoteID: 0,
			},
			swapFees:   1000,
			redeemFees: 1000,
			expectPreOrderParam: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    false,
				Qty:     1e6,
				Rate:    5e7,
			},
		},
		{
			name: "2 lots with refund fees, not account locker",
			rate: 5e7,
			dexBalances: map[uint32]uint64{
				0:  calc.BaseToQuote(5e7, 2e6) + 2000,
				42: 1000,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  42,
				QuoteID: 0,
			},
			expectPreOrderParam: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    false,
				Qty:     2e6,
				Rate:    5e7,
			},
			swapFees:   1000,
			redeemFees: 1000,
			refundFees: 1000,
		},
		{
			name: "1 lot with refund fees, account locker",
			rate: 5e7,
			dexBalances: map[uint32]uint64{
				0:  calc.BaseToQuote(5e7, 2e6) + 2000,
				60: 1000,
			},
			market: &core.Market{
				LotSize: 1e6,
				BaseID:  60,
				QuoteID: 0,
			},
			expectPreOrderParam: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    60,
				Quote:   0,
				Sell:    false,
				Qty:     1e6,
				Rate:    5e7,
			},
			swapFees:   1000,
			redeemFees: 1000,
			refundFees: 1000,
		},
	}

	for _, test := range tests {
		tCore.market = test.market
		tCore.buySwapFees = test.swapFees
		tCore.buyRedeemFees = test.redeemFees

		botID := dcrBtcID
		if test.market.BaseID != 42 {
			botID = ethBtcID
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		adaptor := unifiedExchangeAdaptorForBot(botID, test.dexBalances, nil, tCore, nil, tLogger)
		adaptor.run(ctx)

		res, err := adaptor.MaxBuy("host1", test.market.BaseID, test.market.QuoteID, test.rate)
		if test.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error but did not get", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		if !reflect.DeepEqual(tCore.preOrderParam, test.expectPreOrderParam) {
			t.Fatalf("%s: expected pre order param %+v != actual %+v", test.name, test.expectPreOrderParam, tCore.preOrderParam)
		}

		if !reflect.DeepEqual(res, expectedResult) {
			t.Fatalf("%s: expected max buy result %+v != actual %+v", test.name, expectedResult, res)
		}
	}
}

func TestExchangeAdaptorDEXTrade(t *testing.T) {
	host := "dex.com"

	orderIDs := make([]order.OrderID, 5)
	for i := range orderIDs {
		var id order.OrderID
		copy(id[:], encode.RandomBytes(order.OrderIDSize))
		orderIDs[i] = id
	}

	matchIDs := make([]order.MatchID, 5)
	for i := range matchIDs {
		var id order.MatchID
		copy(id[:], encode.RandomBytes(order.MatchIDSize))
		matchIDs[i] = id
	}

	walletTxIDs := make([][32]byte, 6)
	for i := range walletTxIDs {
		var id [32]byte
		copy(id[:], encode.RandomBytes(32))
		walletTxIDs[i] = id
	}

	type updateAndBalances struct {
		walletTxUpdates []*asset.WalletTransaction
		note            core.Notification
		balances        map[uint32]*botBalance
	}

	type test struct {
		name               string
		isDynamicSwapper   map[uint32]bool
		balances           map[uint32]uint64
		multiTrade         *core.MultiTradeForm
		multiTradeResponse []*core.Order
		wantErr            bool
		postTradeBalances  map[uint32]*botBalance
		updatesAndBalances []*updateAndBalances
	}

	tests := []*test{
		{
			name: "non dynamic swapper, sell",
			balances: map[uint32]uint64{
				42: 1e8,
				0:  1e8,
			},
			multiTrade: &core.MultiTradeForm{
				Host:  host,
				Sell:  true,
				Base:  42,
				Quote: 0,
				Placements: []*core.QtyRate{
					{
						Qty:  5e6,
						Rate: 5e7,
					},
					{
						Qty:  5e6,
						Rate: 6e7,
					},
				},
			},
			multiTradeResponse: []*core.Order{
				{
					Host:      host,
					BaseID:    42,
					QuoteID:   0,
					Sell:      true,
					LockedAmt: 5e6 + 2000,
					Status:    order.OrderStatusBooked,
					ID:        orderIDs[0][:],
				},
				{
					Host:      host,
					BaseID:    42,
					QuoteID:   0,
					Sell:      true,
					LockedAmt: 5e6 + 2000,
					Status:    order.OrderStatusBooked,
					ID:        orderIDs[1][:],
				},
			},
			postTradeBalances: map[uint32]*botBalance{
				42: {
					Available: 1e8 - (5e6+2000)*2,
					Locked:    (5e6 + 2000) * 2,
				},
				0: {
					Available: 1e8,
				},
			},
			updatesAndBalances: []*updateAndBalances{
				// First order has a match and sends a swap tx
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:         asset.Swap,
							ID:           walletTxIDs[0][:],
							BalanceDelta: -2e6,
							Fees:         1000,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:      host,
							BaseID:    42,
							QuoteID:   0,
							Sell:      true,
							LockedAmt: 3e6 + 1000,
							Status:    order.OrderStatusBooked,
							ID:        orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8 - (5e6+2000)*2,
							Locked:    5e6 + 2000 + 3e6 + 1000,
						},
						0: {
							Available: 1e8,
						},
					},
				},
				// Second order has a match and sends swap tx
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:         asset.Swap,
							ID:           walletTxIDs[1][:],
							BalanceDelta: -3e6,
							Fees:         1000,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:      host,
							BaseID:    42,
							QuoteID:   0,
							Sell:      true,
							LockedAmt: 2e6 + 1000,
							Status:    order.OrderStatusBooked,
							ID:        orderIDs[1][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[1][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8 - (5e6+2000)*2,
							Locked:    2e6 + 1000 + 3e6 + 1000,
						},
						0: {
							Available: 1e8,
						},
					},
				},
				// First order swap is confirmed, and redemption is sent
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Swap,
							ID:                  walletTxIDs[0][:],
							BalanceDelta:        -2e6,
							Fees:                1000,
							PartOfActiveBalance: true,
						},
						{
							Type:         asset.Redeem,
							ID:           walletTxIDs[2][:],
							BalanceDelta: int64(calc.BaseToQuote(5e7, 2e6)),
							Fees:         1000,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:      host,
							BaseID:    42,
							QuoteID:   0,
							Sell:      true,
							LockedAmt: 3e6 + 1000,
							Status:    order.OrderStatusBooked,
							ID:        orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[2][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8 - (5e6+2000)*2,
							Locked:    2e6 + 1000 + 3e6 + 1000,
						},
						0: {
							Available: 1e8,
							Pending:   calc.BaseToQuote(5e7, 2e6) - 1000,
						},
					},
				},
				// First order redemption confirmed
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Redeem,
							ID:                  walletTxIDs[2][:],
							BalanceDelta:        int64(calc.BaseToQuote(5e7, 2e6)),
							Fees:                1000,
							PartOfActiveBalance: true,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:      host,
							BaseID:    42,
							QuoteID:   0,
							Sell:      true,
							LockedAmt: 3e6 + 1000,
							Status:    order.OrderStatusBooked,
							ID:        orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[2][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8 - (5e6+2000)*2,
							Locked:    2e6 + 1000 + 3e6 + 1000,
						},
						0: {
							Available: 1e8 + calc.BaseToQuote(5e7, 2e6) - 1000,
						},
					},
				},
				// First order cancelled
				{
					note: &core.OrderNote{
						Order: &core.Order{
							Host:             host,
							BaseID:           42,
							QuoteID:          0,
							Sell:             true,
							Status:           order.OrderStatusCanceled,
							ID:               orderIDs[0][:],
							AllFeesConfirmed: true,
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[2][:],
									},
									Status: order.MatchConfirmed,
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8 - (7e6 + 2000 + 1000),
							Locked:    2e6 + 1000,
						},
						0: {
							Available: 1e8 + calc.BaseToQuote(5e7, 2e6) - 1000,
						},
					},
				},
				// Second order second match, swap sent, and first match refunded
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Swap,
							ID:                  walletTxIDs[1][:],
							BalanceDelta:        -3e6,
							Fees:                1000,
							PartOfActiveBalance: true,
						},
						{
							Type:         asset.Refund,
							ID:           walletTxIDs[3][:],
							BalanceDelta: 3e6,
							Fees:         1200,
						},
						{
							Type:         asset.Swap,
							ID:           walletTxIDs[4][:],
							BalanceDelta: -2e6,
							Fees:         800,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:    host,
							BaseID:  42,
							QuoteID: 0,
							Sell:    true,
							Status:  order.OrderStatusExecuted,
							ID:      orderIDs[1][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[1][:],
									},
									Refund: &core.Coin{
										ID: walletTxIDs[3][:],
									},
								},
								{
									Swap: &core.Coin{
										ID: walletTxIDs[4][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8 - (7e6 + 1800 + 1000),
							Pending:   3e6 - 1200,
						},
						0: {
							Available: 1e8 + calc.BaseToQuote(5e7, 2e6) - 1000,
						},
					},
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Refund,
							ID:                  walletTxIDs[3][:],
							BalanceDelta:        3e6,
							Fees:                1200,
							PartOfActiveBalance: true,
						},
						{
							Type:                asset.Swap,
							ID:                  walletTxIDs[4][:],
							BalanceDelta:        -2e6,
							Fees:                800,
							PartOfActiveBalance: true,
						},
						{
							Type:                asset.Redeem,
							ID:                  walletTxIDs[5][:],
							BalanceDelta:        int64(calc.BaseToQuote(6e7, 2e6)),
							Fees:                700,
							PartOfActiveBalance: true,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:    host,
							BaseID:  42,
							QuoteID: 0,
							Sell:    true,
							Status:  order.OrderStatusExecuted,
							ID:      orderIDs[1][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[1][:],
									},
									Refund: &core.Coin{
										ID: walletTxIDs[3][:],
									},
								},
								{
									Swap: &core.Coin{
										ID: walletTxIDs[4][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[5][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8 - (4e6 + 1800 + 1000 + 1200),
						},
						0: {
							Available: 1e8 + calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 2e6) - 1700,
						},
					},
				},
			},
		},
		{
			name: "non dynamic swapper, buy",
			balances: map[uint32]uint64{
				42: 1e8,
				0:  1e8,
			},
			multiTrade: &core.MultiTradeForm{
				Host:  host,
				Sell:  false,
				Base:  42,
				Quote: 0,
				Placements: []*core.QtyRate{
					{
						Qty:  5e6,
						Rate: 5e7,
					},
					{
						Qty:  5e6,
						Rate: 6e7,
					},
				},
			},
			multiTradeResponse: []*core.Order{
				{
					Host:      host,
					BaseID:    42,
					QuoteID:   0,
					Sell:      false,
					LockedAmt: calc.BaseToQuote(5e7, 5e6) + 2000,
					Status:    order.OrderStatusBooked,
					ID:        orderIDs[0][:],
				},
				{
					Host:      host,
					BaseID:    42,
					QuoteID:   0,
					Sell:      false,
					LockedAmt: calc.BaseToQuote(6e7, 5e6) + 2000,
					Status:    order.OrderStatusBooked,
					ID:        orderIDs[1][:],
				},
			},
			postTradeBalances: map[uint32]*botBalance{
				42: {
					Available: 1e8,
				},
				0: {
					Available: 1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6) + 4000),
					Locked:    calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6) + 4000,
				},
			},
			updatesAndBalances: []*updateAndBalances{
				// First order has a match and sends a swap tx
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:         asset.Swap,
							ID:           walletTxIDs[0][:],
							BalanceDelta: -int64(calc.BaseToQuote(5e7, 2e6)),
							Fees:         1000,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:      host,
							BaseID:    42,
							QuoteID:   0,
							Sell:      false,
							LockedAmt: calc.BaseToQuote(5e7, 3e6) + 1000,
							Status:    order.OrderStatusBooked,
							ID:        orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8,
						},
						0: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6) + 4000),
							Locked:    calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 5e6) + 3000,
						},
					},
				},
				// Second order has a match and sends swap tx
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:         asset.Swap,
							ID:           walletTxIDs[1][:],
							BalanceDelta: -int64(calc.BaseToQuote(6e7, 3e6)),
							Fees:         1000,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:      host,
							BaseID:    42,
							QuoteID:   0,
							Sell:      false,
							LockedAmt: calc.BaseToQuote(6e7, 2e6) + 1000,
							Status:    order.OrderStatusBooked,
							ID:        orderIDs[1][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[1][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8,
						},
						0: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6) + 4000),
							Locked:    calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 2e6) + 2000,
						},
					},
				},
				// First order swap is confirmed, and redemption is sent
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Swap,
							ID:                  walletTxIDs[0][:],
							BalanceDelta:        -int64(calc.BaseToQuote(5e7, 2e6)),
							Fees:                1000,
							PartOfActiveBalance: true,
						},
						{
							Type:         asset.Redeem,
							ID:           walletTxIDs[2][:],
							BalanceDelta: 2e6,
							Fees:         1000,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:      host,
							BaseID:    42,
							QuoteID:   0,
							Sell:      false,
							LockedAmt: calc.BaseToQuote(5e7, 3e6) + 1000,
							Status:    order.OrderStatusBooked,
							ID:        orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[2][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8,
							Pending:   2e6 - 1000,
						},
						0: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6) + 4000),
							Locked:    calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 2e6) + 2000,
						},
					},
				},
				// First order redemption confirmed
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Redeem,
							ID:                  walletTxIDs[2][:],
							BalanceDelta:        2e6,
							Fees:                1000,
							PartOfActiveBalance: true,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:      host,
							BaseID:    42,
							QuoteID:   0,
							Sell:      false,
							LockedAmt: calc.BaseToQuote(5e7, 3e6) + 1000,
							Status:    order.OrderStatusBooked,
							ID:        orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[2][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8 + 2e6 - 1000,
						},
						0: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6) + 4000),
							Locked:    calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 2e6) + 2000,
						},
					},
				},
				// First order cancelled
				{
					note: &core.OrderNote{
						Order: &core.Order{
							Host:    host,
							BaseID:  42,
							QuoteID: 0,
							Sell:    false,
							Status:  order.OrderStatusCanceled,
							ID:      orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[2][:],
									},
									Status: order.MatchConfirmed,
								},
							},
							AllFeesConfirmed: true,
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8 + 2e6 - 1000,
						},
						0: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 5e6) + 3000),
							Locked:    calc.BaseToQuote(6e7, 2e6) + 1000,
						},
					},
				},
				// Second order second match, swap sent, and first match refunded
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Swap,
							ID:                  walletTxIDs[1][:],
							BalanceDelta:        -int64(calc.BaseToQuote(6e7, 3e6)),
							Fees:                1000,
							PartOfActiveBalance: true,
						},
						{
							Type:         asset.Refund,
							ID:           walletTxIDs[3][:],
							BalanceDelta: int64(calc.BaseToQuote(6e7, 3e6)),
							Fees:         1200,
						},
						{
							Type:         asset.Swap,
							ID:           walletTxIDs[4][:],
							BalanceDelta: -int64(calc.BaseToQuote(6e7, 2e6)),
							Fees:         800,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:    host,
							BaseID:  42,
							QuoteID: 0,
							Sell:    false,
							Status:  order.OrderStatusExecuted,
							ID:      orderIDs[1][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[1][:],
									},
									Refund: &core.Coin{
										ID: walletTxIDs[3][:],
									},
								},
								{
									Swap: &core.Coin{
										ID: walletTxIDs[4][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8 + 2e6 - 1000,
						},
						0: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 5e6) + 2800),
							Pending:   calc.BaseToQuote(6e7, 3e6) - 1200,
						},
					},
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Refund,
							ID:                  walletTxIDs[3][:],
							BalanceDelta:        int64(calc.BaseToQuote(6e7, 3e6)),
							Fees:                1200,
							PartOfActiveBalance: true,
						},
						{
							Type:                asset.Swap,
							ID:                  walletTxIDs[4][:],
							BalanceDelta:        -int64(calc.BaseToQuote(6e7, 2e6)),
							Fees:                800,
							PartOfActiveBalance: true,
						},
						{
							Type:                asset.Redeem,
							ID:                  walletTxIDs[5][:],
							BalanceDelta:        2e6,
							Fees:                700,
							PartOfActiveBalance: true,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:    host,
							BaseID:  42,
							QuoteID: 0,
							Sell:    false,
							Status:  order.OrderStatusExecuted,
							ID:      orderIDs[1][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[1][:],
									},
									Refund: &core.Coin{
										ID: walletTxIDs[3][:],
									},
								},
								{
									Swap: &core.Coin{
										ID: walletTxIDs[4][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[5][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e8 + 4e6 - 1700,
						},
						0: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 2e6) + 2800 + 1200),
						},
					},
				},
			},
		},
		{
			name: "dynamic swapper, token, sell",
			isDynamicSwapper: map[uint32]bool{
				966001: true,
				966:    true,
				60:     true,
			},
			balances: map[uint32]uint64{
				966001: 1e8,
				966:    1e8,
				60:     1e8,
			},
			multiTrade: &core.MultiTradeForm{
				Host:  host,
				Sell:  true,
				Base:  60,
				Quote: 966001,
				Placements: []*core.QtyRate{
					{
						Qty:  5e6,
						Rate: 5e7,
					},
					{
						Qty:  5e6,
						Rate: 6e7,
					},
				},
			},
			multiTradeResponse: []*core.Order{
				{
					Host:            host,
					BaseID:          60,
					QuoteID:         966001,
					Sell:            true,
					LockedAmt:       5e6 + 2000,
					RefundLockedAmt: 3000,
					RedeemLockedAmt: 4000,
					Status:          order.OrderStatusBooked,
					ID:              orderIDs[0][:],
				},
				{
					Host:            host,
					BaseID:          60,
					QuoteID:         966001,
					Sell:            true,
					LockedAmt:       5e6 + 2000,
					RefundLockedAmt: 3000,
					RedeemLockedAmt: 4000,
					Status:          order.OrderStatusBooked,
					ID:              orderIDs[1][:],
				},
			},
			postTradeBalances: map[uint32]*botBalance{
				966001: {
					Available: 1e8,
				},
				966: {
					Available: 1e8 - 8000,
					Locked:    8000,
				},
				60: {
					Available: 1e8 - (5e6+2000+3000)*2,
					Locked:    (5e6 + 2000 + 3000) * 2,
				},
			},
			updatesAndBalances: []*updateAndBalances{
				// First order has a match and sends a swap tx
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:         asset.Swap,
							ID:           walletTxIDs[0][:],
							BalanceDelta: -2e6,
							Fees:         1000,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:            host,
							BaseID:          60,
							QuoteID:         966001,
							Sell:            true,
							LockedAmt:       3e6 + 1000,
							RefundLockedAmt: 3000,
							RedeemLockedAmt: 4000,
							Status:          order.OrderStatusBooked,
							ID:              orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8,
						},
						966: {
							Available: 1e8 - 8000,
							Locked:    8000,
						},
						60: {
							Available: 1e8 - (5e6+2000+3000)*2,
							Locked:    3e6 + 1000 + 5e6 + 2000 + 3000 + 3000,
						},
					},
				},
				// Second order has a match and sends swap tx
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:         asset.Swap,
							ID:           walletTxIDs[1][:],
							BalanceDelta: -3e6,
							Fees:         1000,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:            host,
							BaseID:          60,
							QuoteID:         966001,
							Sell:            true,
							LockedAmt:       2e6 + 1000,
							RefundLockedAmt: 3000,
							RedeemLockedAmt: 4000,
							Status:          order.OrderStatusBooked,
							ID:              orderIDs[1][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[1][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8,
						},
						966: {
							Available: 1e8 - 8000,
							Locked:    8000,
						},
						60: {
							Available: 1e8 - (5e6+2000+3000)*2,
							Locked:    3e6 + 1000 + 2e6 + 1000 + 3000 + 3000,
						},
					},
				},
				// First order swap is confirmed, and redemption is sent
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Swap,
							ID:                  walletTxIDs[0][:],
							BalanceDelta:        -2e6,
							Fees:                900,
							PartOfActiveBalance: true,
						},
						{
							Type:         asset.Redeem,
							ID:           walletTxIDs[2][:],
							BalanceDelta: int64(calc.BaseToQuote(5e7, 2e6)),
							Fees:         1000,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:            host,
							BaseID:          60,
							QuoteID:         966001,
							Sell:            true,
							LockedAmt:       3e6 + 1000,
							RefundLockedAmt: 3000,
							RedeemLockedAmt: 3000,
							Status:          order.OrderStatusBooked,
							ID:              orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[2][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8,
							Pending:   calc.BaseToQuote(5e7, 2e6),
						},
						966: {
							Available: 1e8 - 8000,
							Locked:    7000,
						},
						60: {
							Available: 1e8 - (5e6+2000+3000)*2 + 100,
							Locked:    3e6 + 1000 + 2e6 + 1000 + 3000 + 3000,
						},
					},
				},
				// First order redemption confirmed
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Redeem,
							ID:                  walletTxIDs[2][:],
							BalanceDelta:        int64(calc.BaseToQuote(5e7, 2e6)),
							Fees:                800,
							PartOfActiveBalance: true,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:            host,
							BaseID:          60,
							QuoteID:         966001,
							Sell:            true,
							LockedAmt:       3e6 + 1000,
							RefundLockedAmt: 3000,
							RedeemLockedAmt: 3000,
							Status:          order.OrderStatusBooked,
							ID:              orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[2][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8 + calc.BaseToQuote(5e7, 2e6),
						},
						966: {
							Available: 1e8 - 7800,
							Locked:    7000,
						},
						60: {
							Available: 1e8 - (5e6+2000+3000)*2 + 100,
							Locked:    3e6 + 1000 + 2e6 + 1000 + 3000 + 3000,
						},
					},
				},
				// First order cancelled
				{
					note: &core.OrderNote{
						Order: &core.Order{
							Host:             host,
							BaseID:           60,
							QuoteID:          966001,
							Sell:             true,
							Status:           order.OrderStatusCanceled,
							ID:               orderIDs[0][:],
							AllFeesConfirmed: true,
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[2][:],
									},
									Status: order.MatchConfirmed,
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8 + calc.BaseToQuote(5e7, 2e6),
						},
						966: {
							Available: 1e8 - 4800,
							Locked:    4000,
						},
						60: {
							Available: 1e8 - (7e6 + 900 + 2000 + 3000),
							Locked:    2e6 + 1000 + 3000,
						},
					},
				},
				// Second order second match, swap sent, and first match refunded
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Swap,
							ID:                  walletTxIDs[1][:],
							BalanceDelta:        -3e6,
							Fees:                1000,
							PartOfActiveBalance: true,
						},
						{
							Type:         asset.Refund,
							ID:           walletTxIDs[3][:],
							BalanceDelta: 3e6,
							Fees:         1200,
						},
						{
							Type:         asset.Swap,
							ID:           walletTxIDs[4][:],
							BalanceDelta: -2e6,
							Fees:         800,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:            host,
							BaseID:          60,
							QuoteID:         966001,
							Sell:            true,
							Status:          order.OrderStatusExecuted,
							ID:              orderIDs[1][:],
							RefundLockedAmt: 1800,
							RedeemLockedAmt: 4000,
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[1][:],
									},
									Refund: &core.Coin{
										ID: walletTxIDs[3][:],
									},
								},
								{
									Swap: &core.Coin{
										ID: walletTxIDs[4][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8 + calc.BaseToQuote(5e7, 2e6),
						},
						966: {
							Available: 1e8 - 4800,
							Locked:    4000,
						},
						60: {
							Available: 1e8 - (7e6 + 900 + 2000 + 3000) + 200,
							Pending:   3e6,
							Locked:    1800,
						},
					},
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Refund,
							ID:                  walletTxIDs[3][:],
							BalanceDelta:        3e6,
							Fees:                1100,
							PartOfActiveBalance: true,
						},
						{
							Type:                asset.Swap,
							ID:                  walletTxIDs[4][:],
							BalanceDelta:        -2e6,
							Fees:                800,
							PartOfActiveBalance: true,
						},
						{
							Type:                asset.Redeem,
							ID:                  walletTxIDs[5][:],
							BalanceDelta:        int64(calc.BaseToQuote(6e7, 2e6)),
							Fees:                700,
							PartOfActiveBalance: true,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:    host,
							BaseID:  60,
							QuoteID: 966001,
							Sell:    true,
							Status:  order.OrderStatusExecuted,
							ID:      orderIDs[1][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[1][:],
									},
									Refund: &core.Coin{
										ID: walletTxIDs[3][:],
									},
								},
								{
									Swap: &core.Coin{
										ID: walletTxIDs[4][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[5][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8 + calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 2e6),
						},
						966: {
							Available: 1e8 - 1500,
						},
						60: {
							Available: 1e8 - (4e6 + 3800),
						},
					},
				},
			},
		},
		{
			name: "dynamic swapper, token, buy",
			isDynamicSwapper: map[uint32]bool{
				966001: true,
				966:    true,
				60:     true,
			},
			balances: map[uint32]uint64{
				966001: 1e8,
				966:    1e8,
				60:     1e8,
			},
			multiTrade: &core.MultiTradeForm{
				Host:  host,
				Sell:  false,
				Base:  60,
				Quote: 966001,
				Placements: []*core.QtyRate{
					{
						Qty:  5e6,
						Rate: 5e7,
					},
					{
						Qty:  5e6,
						Rate: 6e7,
					},
				},
			},
			multiTradeResponse: []*core.Order{
				{
					Host:                 host,
					BaseID:               60,
					QuoteID:              966001,
					Sell:                 false,
					LockedAmt:            calc.BaseToQuote(5e7, 5e6),
					ParentAssetLockedAmt: 2000,
					RedeemLockedAmt:      3000,
					RefundLockedAmt:      4000,
					Status:               order.OrderStatusBooked,
					ID:                   orderIDs[0][:],
				},
				{
					Host:                 host,
					BaseID:               60,
					QuoteID:              966001,
					Sell:                 false,
					LockedAmt:            calc.BaseToQuote(6e7, 5e6),
					ParentAssetLockedAmt: 2000,
					RedeemLockedAmt:      3000,
					RefundLockedAmt:      4000,
					Status:               order.OrderStatusBooked,
					ID:                   orderIDs[1][:],
				},
			},
			postTradeBalances: map[uint32]*botBalance{
				966001: {
					Available: 1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)),
					Locked:    calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6),
				},
				966: {
					Available: 1e8 - 12000,
					Locked:    12000,
				},
				60: {
					Available: 1e8 - 6000,
					Locked:    6000,
				},
			},
			updatesAndBalances: []*updateAndBalances{
				// First order has a match and sends a swap tx
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:         asset.Swap,
							ID:           walletTxIDs[0][:],
							BalanceDelta: -int64(calc.BaseToQuote(5e7, 2e6)),
							Fees:         1000,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:                 host,
							BaseID:               60,
							QuoteID:              966001,
							Sell:                 false,
							LockedAmt:            calc.BaseToQuote(5e7, 3e6),
							ParentAssetLockedAmt: 1000,
							RedeemLockedAmt:      3000,
							RefundLockedAmt:      4000,
							Status:               order.OrderStatusBooked,
							ID:                   orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)),
							Locked:    calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 5e6),
						},
						966: {
							Available: 1e8 - 12000,
							Locked:    11000,
						},
						60: {
							Available: 1e8 - 6000,
							Locked:    6000,
						},
					},
				},
				// Second order has a match and sends swap tx
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:         asset.Swap,
							ID:           walletTxIDs[1][:],
							BalanceDelta: -int64(calc.BaseToQuote(6e7, 3e6)),
							Fees:         1000,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:                 host,
							BaseID:               60,
							QuoteID:              966001,
							Sell:                 false,
							LockedAmt:            calc.BaseToQuote(6e7, 2e6),
							ParentAssetLockedAmt: 1000,
							RedeemLockedAmt:      3000,
							RefundLockedAmt:      4000,
							Status:               order.OrderStatusBooked,
							ID:                   orderIDs[1][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[1][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)),
							Locked:    calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 2e6),
						},
						966: {
							Available: 1e8 - 12000,
							Locked:    10000,
						},
						60: {
							Available: 1e8 - 6000,
							Locked:    6000,
						},
					},
				},
				// First order swap is confirmed, and redemption is sent
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Swap,
							ID:                  walletTxIDs[0][:],
							BalanceDelta:        -int64(calc.BaseToQuote(5e7, 2e6)),
							Fees:                900,
							PartOfActiveBalance: true,
						},
						{
							Type:         asset.Redeem,
							ID:           walletTxIDs[2][:],
							BalanceDelta: 2e6,
							Fees:         1000,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:                 host,
							BaseID:               60,
							QuoteID:              966001,
							Sell:                 false,
							LockedAmt:            calc.BaseToQuote(5e7, 3e6),
							ParentAssetLockedAmt: 1000,
							RedeemLockedAmt:      2000,
							RefundLockedAmt:      4000,
							Status:               order.OrderStatusBooked,
							ID:                   orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[2][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)),
							Locked:    calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 2e6),
						},
						966: {
							Available: 1e8 - 12000 + 100,
							Locked:    10000,
						},
						60: {
							Available: 1e8 - 6000,
							Pending:   2e6,
							Locked:    5000,
						},
					},
				},
				// First order redemption confirmed
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Redeem,
							ID:                  walletTxIDs[2][:],
							BalanceDelta:        2e6,
							Fees:                800,
							PartOfActiveBalance: true,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:                 host,
							BaseID:               60,
							QuoteID:              966001,
							Sell:                 false,
							LockedAmt:            calc.BaseToQuote(5e7, 3e6),
							ParentAssetLockedAmt: 1000,
							RedeemLockedAmt:      2000,
							RefundLockedAmt:      4000,
							Status:               order.OrderStatusBooked,
							ID:                   orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[2][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)),
							Locked:    calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 2e6),
						},
						966: {
							Available: 1e8 - 12000 + 100,
							Locked:    10000,
						},
						60: {
							Available: 1e8 - 5800 + 2e6,
							Locked:    5000,
						},
					},
				},
				// First order cancelled
				{
					note: &core.OrderNote{
						Order: &core.Order{
							Host:    host,
							BaseID:  60,
							QuoteID: 966001,
							Sell:    false,
							Status:  order.OrderStatusCanceled,
							ID:      orderIDs[0][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[0][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[2][:],
									},
									Status: order.MatchConfirmed,
								},
							},
							AllFeesConfirmed: true,
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 5e6)),
							Locked:    calc.BaseToQuote(6e7, 2e6),
						},
						966: {
							Available: 1e8 - 7000 + 100,
							Locked:    5000,
						},
						60: {
							Available: 1e8 + 2e6 - 3800,
							Locked:    3000,
						},
					},
				},
				// Second order second match, swap sent, and first match refunded
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Swap,
							ID:                  walletTxIDs[1][:],
							BalanceDelta:        -int64(calc.BaseToQuote(6e7, 3e6)),
							Fees:                1000,
							PartOfActiveBalance: true,
						},
						{
							Type:         asset.Refund,
							ID:           walletTxIDs[3][:],
							BalanceDelta: int64(calc.BaseToQuote(6e7, 3e6)),
							Fees:         1200,
						},
						{
							Type:         asset.Swap,
							ID:           walletTxIDs[4][:],
							BalanceDelta: -int64(calc.BaseToQuote(6e7, 2e6)),
							Fees:         800,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:            host,
							BaseID:          60,
							QuoteID:         966001,
							Sell:            false,
							Status:          order.OrderStatusExecuted,
							RedeemLockedAmt: 3000,
							RefundLockedAmt: 2000,
							ID:              orderIDs[1][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[1][:],
									},
									Refund: &core.Coin{
										ID: walletTxIDs[3][:],
									},
								},
								{
									Swap: &core.Coin{
										ID: walletTxIDs[4][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 5e6)),
							Pending:   calc.BaseToQuote(6e7, 3e6),
						},
						966: {
							Available: 1e8 - 6000 + 100,
							Locked:    2000,
						},
						60: {
							Available: 1e8 + 2e6 - 3800,
							Locked:    3000,
						},
					},
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					walletTxUpdates: []*asset.WalletTransaction{
						{
							Type:                asset.Refund,
							ID:                  walletTxIDs[3][:],
							BalanceDelta:        int64(calc.BaseToQuote(6e7, 3e6)),
							Fees:                1200,
							PartOfActiveBalance: true,
						},
						{
							Type:                asset.Swap,
							ID:                  walletTxIDs[4][:],
							BalanceDelta:        -int64(calc.BaseToQuote(6e7, 2e6)),
							Fees:                800,
							PartOfActiveBalance: true,
						},
						{
							Type:                asset.Redeem,
							ID:                  walletTxIDs[5][:],
							BalanceDelta:        2e6,
							Fees:                700,
							PartOfActiveBalance: true,
						},
					},
					note: &core.OrderNote{
						Order: &core.Order{
							Host:    host,
							BaseID:  60,
							QuoteID: 966001,
							Sell:    false,
							Status:  order.OrderStatusExecuted,
							ID:      orderIDs[1][:],
							Matches: []*core.Match{
								{
									Swap: &core.Coin{
										ID: walletTxIDs[1][:],
									},
									Refund: &core.Coin{
										ID: walletTxIDs[3][:],
									},
								},
								{
									Swap: &core.Coin{
										ID: walletTxIDs[4][:],
									},
									Redeem: &core.Coin{
										ID: walletTxIDs[5][:],
									},
								},
							},
						},
					},
					balances: map[uint32]*botBalance{
						966001: {
							Available: 1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 2e6)),
						},
						966: {
							Available: 1e8 - 3900,
						},
						60: {
							Available: 1e8 + 4e6 - 1500,
						},
					},
				},
			},
		},
	}

	mkt := &core.Market{
		LotSize: 1e6,
	}

	runTest := func(test *test) {
		tCore := newTCore()
		tCore.multiTradeResult = test.multiTradeResponse
		tCore.market = mkt
		tCore.isDynamicSwapper = test.isDynamicSwapper

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		botID := dexMarketID(test.multiTrade.Host, test.multiTrade.Base, test.multiTrade.Quote)
		adaptor := unifiedExchangeAdaptorForBot(botID, test.balances, nil, tCore, nil, tLogger)
		adaptor.run(ctx)
		_, err := adaptor.MultiTrade([]byte{}, test.multiTrade)
		if test.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error but did not get", test.name)
			}
			return
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		checkBalances := func(expected map[uint32]*botBalance, updateNum int) {
			t.Helper()
			for assetID, expectedBal := range expected {
				bal, err := adaptor.AssetBalance(assetID)
				if err != nil {
					t.Fatalf("%s: unexpected error: %v", test.name, err)
				}
				if *bal != *expectedBal {
					var updateStr string
					if updateNum <= 0 {
						updateStr = "post trade"
					} else {
						updateStr = fmt.Sprintf("after update #%d", updateNum)
					}
					t.Fatalf("%s: unexpected asset %d balance %s. want %+v, got %+v",
						test.name, assetID, updateStr, expectedBal, bal)
				}
			}
		}

		checkBalances(test.postTradeBalances, 0)

		for i, update := range test.updatesAndBalances {
			for _, txUpdate := range update.walletTxUpdates {
				tCore.walletTxs[hex.EncodeToString(txUpdate.ID)] = txUpdate
			}
			tCore.noteFeed <- update.note
			tCore.noteFeed <- &core.BondPostNote{} // dummy note
			checkBalances(update.balances, i+1)
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}

func TestExchangeAdaptorDeposit(t *testing.T) {
	type test struct {
		name              string
		isWithdrawer      bool
		isDynamicSwapper  bool
		depositAmt        uint64
		sendCoin          *tCoin
		unconfirmedTx     *asset.WalletTransaction
		confirmedTx       *asset.WalletTransaction
		receivedAmt       uint64
		initialDEXBalance uint64
		initialCEXBalance uint64
		assetID           uint32

		preConfirmDEXBalance  *botBalance
		preConfirmCEXBalance  *botBalance
		postConfirmDEXBalance *botBalance
		postConfirmCEXBalance *botBalance
	}

	id := encode.RandomBytes(32)

	tests := []test{
		{
			name:         "withdrawer, not dynamic swapper",
			assetID:      42,
			isWithdrawer: true,
			depositAmt:   1e6,
			sendCoin: &tCoin{
				txID:  id,
				value: 1e6 - 2000,
			},
			unconfirmedTx: &asset.WalletTransaction{
				ID:                  id,
				BalanceDelta:        -(1e6 - 2000),
				Fees:                2000,
				PartOfActiveBalance: false,
			},
			confirmedTx: &asset.WalletTransaction{
				ID:                  id,
				BalanceDelta:        -(1e6 - 2000),
				Fees:                2000,
				PartOfActiveBalance: true,
			},
			receivedAmt:       1e6 - 2000,
			initialDEXBalance: 3e6,
			initialCEXBalance: 1e6,
			preConfirmDEXBalance: &botBalance{
				Available: 2e6,
			},
			preConfirmCEXBalance: &botBalance{
				Available: 1e6,
				Pending:   1e6 - 2000,
			},
			postConfirmDEXBalance: &botBalance{
				Available: 2e6,
			},
			postConfirmCEXBalance: &botBalance{
				Available: 2e6 - 2000,
			},
		},
		{
			name:       "not withdrawer, not dynamic swapper",
			assetID:    42,
			depositAmt: 1e6,
			sendCoin: &tCoin{
				txID:  id,
				value: 1e6,
			},
			unconfirmedTx: &asset.WalletTransaction{
				ID:                  id,
				BalanceDelta:        -1e6,
				Fees:                2000,
				PartOfActiveBalance: false,
			},
			confirmedTx: &asset.WalletTransaction{
				ID:                  id,
				BalanceDelta:        -1e6,
				Fees:                2000,
				PartOfActiveBalance: true,
			},
			receivedAmt:       1e6,
			initialDEXBalance: 3e6,
			initialCEXBalance: 1e6,
			preConfirmDEXBalance: &botBalance{
				Available: 2e6 - 2000,
			},
			preConfirmCEXBalance: &botBalance{
				Available: 1e6,
				Pending:   1e6,
			},
			postConfirmDEXBalance: &botBalance{
				Available: 2e6 - 2000,
			},
			postConfirmCEXBalance: &botBalance{
				Available: 2e6,
			},
		},
		{
			name:             "not withdrawer, dynamic swapper",
			assetID:          42,
			isDynamicSwapper: true,
			depositAmt:       1e6,
			sendCoin: &tCoin{
				txID:  id,
				value: 1e6,
			},
			unconfirmedTx: &asset.WalletTransaction{
				ID:                  id,
				BalanceDelta:        -1e6,
				Fees:                4000,
				PartOfActiveBalance: false,
			},
			confirmedTx: &asset.WalletTransaction{
				ID:                  id,
				BalanceDelta:        -1e6,
				Fees:                2000,
				PartOfActiveBalance: true,
			},
			receivedAmt:       1e6,
			initialDEXBalance: 3e6,
			initialCEXBalance: 1e6,
			preConfirmDEXBalance: &botBalance{
				Available: 2e6 - 4000,
			},
			preConfirmCEXBalance: &botBalance{
				Available: 1e6,
				Pending:   1e6,
			},
			postConfirmDEXBalance: &botBalance{
				Available: 2e6 - 2000,
			},
			postConfirmCEXBalance: &botBalance{
				Available: 2e6,
			},
		},
		{
			name:             "not withdrawer, dynamic swapper, token",
			assetID:          966001,
			isDynamicSwapper: true,
			depositAmt:       1e6,
			sendCoin: &tCoin{
				txID:  id,
				value: 1e6,
			},
			unconfirmedTx: &asset.WalletTransaction{
				ID:                  id,
				BalanceDelta:        -1e6,
				Fees:                4000,
				PartOfActiveBalance: false,
			},
			confirmedTx: &asset.WalletTransaction{
				ID:                  id,
				BalanceDelta:        -1e6,
				Fees:                2000,
				PartOfActiveBalance: true,
			},
			receivedAmt:       1e6,
			initialDEXBalance: 3e6,
			initialCEXBalance: 1e6,
			preConfirmDEXBalance: &botBalance{
				Available: 2e6,
			},
			preConfirmCEXBalance: &botBalance{
				Available: 1e6,
				Pending:   1e6,
			},
			postConfirmDEXBalance: &botBalance{
				Available: 2e6,
			},
			postConfirmCEXBalance: &botBalance{
				Available: 2e6,
			},
		},
	}

	runTest := func(test *test) {
		tCore := newTCore()
		tCore.isWithdrawer[test.assetID] = test.isWithdrawer
		tCore.isDynamicSwapper[test.assetID] = test.isDynamicSwapper
		tCore.setAssetBalances(map[uint32]uint64{test.assetID: test.initialDEXBalance, 0: 2e6, 966: 2e6})
		tCore.walletTxs[hex.EncodeToString(test.unconfirmedTx.ID)] = test.unconfirmedTx
		tCore.sendCoin = test.sendCoin

		tCEX := newTCEX()
		tCEX.balances[test.assetID] = &libxc.ExchangeBalance{
			Available: test.initialCEXBalance,
		}
		tCEX.balances[0] = &libxc.ExchangeBalance{
			Available: 2e6,
		}
		tCEX.balances[966] = &libxc.ExchangeBalance{
			Available: 1e8,
		}

		dexBalances := map[uint32]uint64{
			test.assetID: test.initialDEXBalance,
			0:            2e6,
			966:          2e6,
		}
		cexBalances := map[uint32]uint64{
			0:   2e6,
			966: 1e8,
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		botID := dexMarketID("host1", test.assetID, 0)
		adaptor := unifiedExchangeAdaptorForBot(botID, dexBalances, cexBalances, tCore, tCEX, tLogger)
		adaptor.run(ctx)

		err := adaptor.Deposit(ctx, test.assetID, test.depositAmt, func() {})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		preConfirmBal, err := adaptor.AssetBalance(test.assetID)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if *preConfirmBal != *test.preConfirmDEXBalance {
			t.Fatalf("%s: unexpected pre confirm dex balance. want %d, got %d", test.name, test.preConfirmDEXBalance, preConfirmBal.Available)
		}

		if test.assetID == 966001 {
			preConfirmParentBal, err := adaptor.AssetBalance(966)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", test.name, err)
			}
			if preConfirmParentBal.Available != 2e6-test.unconfirmedTx.Fees {
				t.Fatalf("%s: unexpected pre confirm dex balance. want %d, got %d", test.name, test.preConfirmDEXBalance, preConfirmBal.Available)
			}
		}

		tCore.walletTxs[hex.EncodeToString(test.unconfirmedTx.ID)] = test.confirmedTx
		tCEX.confirmDeposit <- test.receivedAmt
		<-tCEX.confirmDepositComplete

		if test.isDynamicSwapper {
			tCore.confirmWalletTx <- true
			<-tCore.confirmWalletTxComplete
		}

		postConfirmBal, err := adaptor.AssetBalance(test.assetID)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if *postConfirmBal != *test.postConfirmDEXBalance {
			t.Fatalf("%s: unexpected post confirm dex balance. want %d, got %d", test.name, test.postConfirmDEXBalance, postConfirmBal)
		}

		if test.assetID == 966001 {
			postConfirmParentBal, err := adaptor.AssetBalance(966)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", test.name, err)
			}
			if postConfirmParentBal.Available != 2e6-test.confirmedTx.Fees {
				t.Fatalf("%s: unexpected pre confirm dex balance. want %d, got %d", test.name, test.preConfirmDEXBalance, preConfirmBal.Available)
			}
		}
	}

	for _, test := range tests {
		runTest(&test)
	}
}

func TestExchangeAdaptorWithdraw(t *testing.T) {
	assetID := uint32(42)
	id := encode.RandomBytes(32)

	type test struct {
		name              string
		withdrawAmt       uint64
		tx                *asset.WalletTransaction
		initialDEXBalance uint64
		initialCEXBalance uint64

		preConfirmDEXBalance  *botBalance
		preConfirmCEXBalance  *botBalance
		postConfirmDEXBalance *botBalance
		postConfirmCEXBalance *botBalance
	}

	tests := []test{
		{
			name:        "ok",
			withdrawAmt: 1e6,
			tx: &asset.WalletTransaction{
				ID:                  id,
				BalanceDelta:        1e6 - 2000,
				Fees:                2000,
				PartOfActiveBalance: true,
			},
			initialCEXBalance: 3e6,
			initialDEXBalance: 1e6,
			preConfirmDEXBalance: &botBalance{
				Available: 1e6,
				Pending:   1e6,
			},
			preConfirmCEXBalance: &botBalance{
				Available: 2e6,
			},
			postConfirmDEXBalance: &botBalance{
				Available: 2e6 - 2000,
			},
			postConfirmCEXBalance: &botBalance{
				Available: 2e6,
			},
		},
	}

	runTest := func(test *test) {
		tCore := newTCore()
		tCore.walletTxs[hex.EncodeToString(test.tx.ID)] = test.tx

		tCEX := newTCEX()

		dexBalances := map[uint32]uint64{
			assetID: test.initialDEXBalance,
			0:       2e6,
		}
		cexBalances := map[uint32]uint64{
			assetID: test.initialCEXBalance,
			966:     1e8,
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		botID := dexMarketID("host1", assetID, 0)
		adaptor := unifiedExchangeAdaptorForBot(botID, dexBalances, cexBalances, tCore, tCEX, tLogger)
		adaptor.run(ctx)

		err := adaptor.Withdraw(ctx, assetID, test.withdrawAmt, func() {})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		preConfirmBal, err := adaptor.AssetBalance(assetID)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if *preConfirmBal != *test.preConfirmDEXBalance {
			t.Fatalf("%s: unexpected pre confirm dex balance. want %+v, got %+v", test.name, test.preConfirmDEXBalance, preConfirmBal)
		}

		tCEX.confirmWithdrawal <- &withdrawArgs{
			assetID: assetID,
			amt:     test.withdrawAmt,
			txID:    hex.EncodeToString(test.tx.ID),
		}

		<-tCEX.confirmWithdrawalComplete

		postConfirmBal, err := adaptor.AssetBalance(assetID)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if *postConfirmBal != *test.postConfirmDEXBalance {
			t.Fatalf("%s: unexpected post confirm dex balance. want %+v, got %+v", test.name, test.postConfirmDEXBalance, postConfirmBal)
		}
	}

	for _, test := range tests {
		runTest(&test)
	}
}

func TestExchangeAdaptorTrade(t *testing.T) {
	baseID := uint32(42)
	quoteID := uint32(0)
	tradeID := "123"

	type updateAndBalance struct {
		update   *libxc.Trade
		balances map[uint32]*botBalance
	}

	type test struct {
		name     string
		sell     bool
		rate     uint64
		qty      uint64
		balances map[uint32]uint64

		wantErr            bool
		postTradeBalances  map[uint32]*botBalance
		updatesAndBalances []*updateAndBalance
	}

	tests := []*test{
		{
			name: "fully filled sell",
			sell: true,
			rate: 5e7,
			qty:  5e6,
			balances: map[uint32]uint64{
				42: 1e7,
				0:  1e7,
			},
			postTradeBalances: map[uint32]*botBalance{
				42: {
					Available: 5e6,
					Locked:    5e6,
				},
				0: {
					Available: 1e7,
				},
			},
			updatesAndBalances: []*updateAndBalance{
				{
					update: &libxc.Trade{
						ID:          tradeID,
						Sell:        true,
						BaseID:      baseID,
						QuoteID:     quoteID,
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  3e6,
						QuoteFilled: 1.6e6,
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 5e6,
							Locked:    5e6 - 3e6,
						},
						0: {
							Available: 1e7 + 1.6e6,
						},
					},
				},
				{
					update: &libxc.Trade{
						ID:          tradeID,
						Sell:        true,
						BaseID:      baseID,
						QuoteID:     quoteID,
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  5e6,
						QuoteFilled: 2.8e6,
						Complete:    true,
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 5e6,
						},
						0: {
							Available: 1e7 + 2.8e6,
						},
					},
				},
				{
					update: &libxc.Trade{
						ID:          tradeID,
						Sell:        true,
						BaseID:      baseID,
						QuoteID:     quoteID,
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  5e6,
						QuoteFilled: 2.8e6,
						Complete:    true,
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 5e6,
						},
						0: {
							Available: 1e7 + 2.8e6,
						},
					},
				},
			},
		},
		{
			name: "partially filled sell",
			sell: true,
			rate: 5e7,
			qty:  5e6,
			balances: map[uint32]uint64{
				42: 1e7,
				0:  1e7,
			},
			postTradeBalances: map[uint32]*botBalance{
				42: {
					Available: 5e6,
					Locked:    5e6,
				},
				0: {
					Available: 1e7,
				},
			},
			updatesAndBalances: []*updateAndBalance{
				{
					update: &libxc.Trade{
						ID:          tradeID,
						Sell:        true,
						BaseID:      baseID,
						QuoteID:     quoteID,
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  3e6,
						QuoteFilled: 1.6e6,
						Complete:    true,
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 7e6,
						},
						0: {
							Available: 1e7 + 1.6e6,
						},
					},
				},
			},
		},
		{
			name: "fully filled buy",
			sell: false,
			rate: 5e7,
			qty:  5e6,
			balances: map[uint32]uint64{
				42: 1e7,
				0:  1e7,
			},
			postTradeBalances: map[uint32]*botBalance{
				42: {
					Available: 1e7,
				},
				0: {
					Available: 1e7 - calc.BaseToQuote(5e7, 5e6),
					Locked:    calc.BaseToQuote(5e7, 5e6),
				},
			},
			updatesAndBalances: []*updateAndBalance{
				{
					update: &libxc.Trade{
						ID:          tradeID,
						Sell:        false,
						BaseID:      baseID,
						QuoteID:     quoteID,
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  3e6,
						QuoteFilled: 1.6e6,
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e7 + 3e6,
						},
						0: {
							Available: 1e7 - calc.BaseToQuote(5e7, 5e6),
							Locked:    calc.BaseToQuote(5e7, 5e6) - 1.6e6,
						},
					},
				},
				{
					update: &libxc.Trade{
						ID:          tradeID,
						Sell:        false,
						BaseID:      baseID,
						QuoteID:     quoteID,
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  5.1e6,
						QuoteFilled: calc.BaseToQuote(5e7, 5e6),
						Complete:    true,
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e7 + 5.1e6,
						},
						0: {
							Available: 1e7 - calc.BaseToQuote(5e7, 5e6),
						},
					},
				},
				{
					update: &libxc.Trade{
						ID:          tradeID,
						Sell:        false,
						BaseID:      baseID,
						QuoteID:     quoteID,
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  5.1e6,
						QuoteFilled: calc.BaseToQuote(5e7, 5e6),
						Complete:    true,
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e7 + 5.1e6,
						},
						0: {
							Available: 1e7 - calc.BaseToQuote(5e7, 5e6),
						},
					},
				},
			},
		},
		{
			name: "partially filled buy",
			sell: false,
			rate: 5e7,
			qty:  5e6,
			balances: map[uint32]uint64{
				42: 1e7,
				0:  1e7,
			},
			postTradeBalances: map[uint32]*botBalance{
				42: {
					Available: 1e7,
				},
				0: {
					Available: 1e7 - calc.BaseToQuote(5e7, 5e6),
					Locked:    calc.BaseToQuote(5e7, 5e6),
				},
			},
			updatesAndBalances: []*updateAndBalance{
				{
					update: &libxc.Trade{
						ID:          tradeID,
						Sell:        false,
						BaseID:      baseID,
						QuoteID:     quoteID,
						Rate:        5e7,
						Qty:         5e6,
						BaseFilled:  3e6,
						QuoteFilled: 1.6e6,
						Complete:    true,
					},
					balances: map[uint32]*botBalance{
						42: {
							Available: 1e7 + 3e6,
						},
						0: {
							Available: 1e7 - 1.6e6,
						},
					},
				},
			},
		},
	}

	botCfg := &BotConfig{
		Host:             "host1",
		BaseAsset:        baseID,
		QuoteAsset:       quoteID,
		BaseBalanceType:  Percentage,
		BaseBalance:      100,
		QuoteBalanceType: Percentage,
		QuoteBalance:     100,
		CEXCfg: &BotCEXCfg{
			Name:             "Binance",
			BaseBalanceType:  Percentage,
			BaseBalance:      100,
			QuoteBalanceType: Percentage,
			QuoteBalance:     100,
		},
	}

	runTest := func(test *test) {
		tCore := newTCore()
		tCEX := newTCEX()
		tCEX.tradeID = tradeID

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		botID := dexMarketID(botCfg.Host, botCfg.BaseAsset, botCfg.QuoteAsset)
		adaptor := unifiedExchangeAdaptorForBot(botID, test.balances, test.balances, tCore, tCEX, tLogger)
		adaptor.run(ctx)

		adaptor.SubscribeTradeUpdates()

		_, err := adaptor.Trade(ctx, baseID, quoteID, test.sell, test.rate, test.qty)
		if test.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error but did not get", test.name)
			}
			return
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		checkBalances := func(expected map[uint32]*botBalance, i int) {
			t.Helper()
			for assetID, expectedBal := range expected {
				bal, err := adaptor.Balance(assetID)
				if err != nil {
					t.Fatalf("%s: unexpected error: %v", test.name, err)
				}
				if *bal != *expectedBal {
					step := "post trade"
					if i > 0 {
						step = fmt.Sprintf("after update #%d", i)
					}
					t.Fatalf("%s: unexpected cex balance %s for asset %d. want %+v, got %+v",
						test.name, step, assetID, expectedBal, bal)
				}
			}
		}

		checkBalances(test.postTradeBalances, 0)

		for i, updateAndBalance := range test.updatesAndBalances {
			tCEX.tradeUpdates <- updateAndBalance.update
			tCEX.tradeUpdates <- &libxc.Trade{} // dummy update
			checkBalances(updateAndBalance.balances, i+1)
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}
