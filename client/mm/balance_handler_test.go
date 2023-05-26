package mm

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

var tLogger = dex.StdOutLogger("mm_TEST", dex.LevelTrace)

func TestNewBalanceHandler(t *testing.T) {
	tCore := newTCore()

	dcrBtcID := fmt.Sprintf("%s-%d-%d", "host1", 42, 0)
	dcrEthID := fmt.Sprintf("%s-%d-%d", "host1", 42, 60)

	tests := []struct {
		name          string
		cfgs          []*BotConfig
		assetBalances map[uint32]uint64

		wantReserves map[string]map[uint32]uint64
		wantErr      bool
	}{
		{
			name: "percentages only, ok",
			cfgs: []*BotConfig{
				{
					Host:             "host1",
					BaseAsset:        42,
					QuoteAsset:       0,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,
				},
				{
					Host:             "host1",
					BaseAsset:        42,
					QuoteAsset:       60,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     100,
				},
			},

			assetBalances: map[uint32]uint64{
				0:  1000,
				42: 1000,
				60: 2000,
			},

			wantReserves: map[string]map[uint32]uint64{
				dcrBtcID: {
					0:  500,
					42: 500,
				},
				dcrEthID: {
					42: 500,
					60: 2000,
				},
			},
		},

		{
			name: "50% + 51% error",
			cfgs: []*BotConfig{
				{
					Host:             "host1",
					BaseAsset:        42,
					QuoteAsset:       0,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,
				},
				{
					Host:             "host1",
					BaseAsset:        42,
					QuoteAsset:       60,
					BaseBalanceType:  Percentage,
					BaseBalance:      51,
					QuoteBalanceType: Percentage,
					QuoteBalance:     100,
				},
			},

			assetBalances: map[uint32]uint64{
				0:  1000,
				42: 1000,
				60: 2000,
			},

			wantErr: true,
		},

		{
			name: "combine amount and percentages, ok",
			cfgs: []*BotConfig{
				{
					Host:             "host1",
					BaseAsset:        42,
					QuoteAsset:       0,
					BaseBalanceType:  Amount,
					BaseBalance:      499,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,
				},
				{
					Host:             "host1",
					BaseAsset:        42,
					QuoteAsset:       60,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     100,
				},
			},

			assetBalances: map[uint32]uint64{
				0:  1000,
				42: 1000,
				60: 2000,
			},

			wantReserves: map[string]map[uint32]uint64{
				dcrBtcID: {
					0:  500,
					42: 499,
				},
				dcrEthID: {
					42: 500,
					60: 2000,
				},
			},
		},
		{
			name: "combine amount and percentages, too high error",
			cfgs: []*BotConfig{
				{
					Host:             "host1",
					BaseAsset:        42,
					QuoteAsset:       0,
					BaseBalanceType:  Amount,
					BaseBalance:      501,
					QuoteBalanceType: Percentage,
					QuoteBalance:     50,
				},
				{
					Host:             "host1",
					BaseAsset:        42,
					QuoteAsset:       60,
					BaseBalanceType:  Percentage,
					BaseBalance:      50,
					QuoteBalanceType: Percentage,
					QuoteBalance:     100,
				},
			},

			assetBalances: map[uint32]uint64{
				0:  1000,
				42: 1000,
				60: 2000,
			},

			wantErr: true,
		},
	}

	for _, test := range tests {
		tCore.setAssetBalances(test.assetBalances)

		bh, err := newBalanceHandler(test.cfgs, tCore, tLogger)
		if test.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error, got nil", test.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		for id, reserves := range bh.botBalances {
			fmt.Printf("bot %s reserves: %d \n", id, reserves)
		}

		for botID, wantReserve := range test.wantReserves {
			botReserves := bh.botBalances[botID]
			for assetID, wantReserve := range wantReserve {
				if botReserves.balances[assetID] != wantReserve {
					t.Fatalf("%s: unexpected reserve for bot %s, asset %d. "+
						"want %d, got %d", test.name, botID, assetID, wantReserve,
						botReserves.balances[assetID])
				}
			}
		}
	}
}

func TestSegregatedCoreMaxSell(t *testing.T) {
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
		cfg           *BotConfig
		assetBalances map[uint32]uint64
		market        *core.Market
		swapFees      uint64
		redeemFees    uint64

		expectPreOrderParam *core.TradeForm
		wantErr             bool
	}{
		{
			name: "ok",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Percentage,
				BaseBalance:      50,
				QuoteBalanceType: Percentage,
				QuoteBalance:     50,
			},
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			market: &core.Market{
				LotSize: 1e6,
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
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      1e6 + 1000,
				QuoteBalanceType: Amount,
				QuoteBalance:     1000,
			},
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			market: &core.Market{
				LotSize: 1e6,
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
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      1e6 + 999,
				QuoteBalanceType: Amount,
				QuoteBalance:     1000,
			},
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			market: &core.Market{
				LotSize: 1e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			wantErr:    true,
		},
		{
			name: "not enough for 1 lot of redeem fees",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       60,
				BaseBalanceType:  Amount,
				BaseBalance:      1e6 + 1000,
				QuoteBalanceType: Amount,
				QuoteBalance:     999,
			},
			assetBalances: map[uint32]uint64{
				42: 1e7,
				60: 1e7,
			},
			market: &core.Market{
				LotSize: 1e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			wantErr:    true,
		},
		{
			name: "redeem fees don't matter if not account locker",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      1e6 + 1000,
				QuoteBalanceType: Amount,
				QuoteBalance:     999,
			},
			assetBalances: map[uint32]uint64{
				42: 1e7,
				0:  1e7,
			},
			market: &core.Market{
				LotSize: 1e6,
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
	}

	for _, test := range tests {
		tCore.setAssetBalances(test.assetBalances)
		tCore.market = test.market
		tCore.sellSwapFees = test.swapFees
		tCore.sellRedeemFees = test.redeemFees

		bh, err := newBalanceHandler([]*BotConfig{test.cfg}, tCore, tLogger)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		mkt := dcrBtcID
		if test.cfg.QuoteAsset == 60 {
			mkt = dcrEthID
		}

		segregatedCore := bh.wrappedCoreForBot(mkt)
		res, err := segregatedCore.MaxSell("host1", test.cfg.BaseAsset, test.cfg.QuoteAsset)
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

func TestSegregatedCoreMaxBuy(t *testing.T) {
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
		name          string
		cfg           *BotConfig
		assetBalances map[uint32]uint64
		market        *core.Market
		rate          uint64
		swapFees      uint64
		redeemFees    uint64

		expectPreOrderParam *core.TradeForm
		wantErr             bool
	}{
		{
			name: "ok",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Percentage,
				BaseBalance:      50,
				QuoteBalanceType: Percentage,
				QuoteBalance:     50,
			},
			rate: 5e7,
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			market: &core.Market{
				LotSize: 1e6,
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
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      1000,
				QuoteBalanceType: Amount,
				QuoteBalance:     (1e6 * 5e7 / 1e8) + 1000,
			},
			rate: 5e7,
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			market: &core.Market{
				LotSize: 1e6,
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
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      1000,
				QuoteBalanceType: Amount,
				QuoteBalance:     (1e6 * 5e7 / 1e8) + 999,
			},
			rate: 5e7,
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			market: &core.Market{
				LotSize: 1e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			wantErr:    true,
		},
		{
			name: "not enough for 1 lot of redeem fees",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        60,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      999,
				QuoteBalanceType: Amount,
				QuoteBalance:     (1e6 * 5e7 / 1e8) + 1000,
			},
			rate: 5e7,
			assetBalances: map[uint32]uint64{
				0:  1e7,
				60: 1e7,
			},
			market: &core.Market{
				LotSize: 1e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			wantErr:    true,
		},
		{
			name: "only account locker affected by redeem fees",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      999,
				QuoteBalanceType: Amount,
				QuoteBalance:     (1e6 * 5e7 / 1e8) + 1000,
			},
			rate: 5e7,
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			market: &core.Market{
				LotSize: 1e6,
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
	}

	for _, test := range tests {
		tCore.setAssetBalances(test.assetBalances)
		tCore.market = test.market
		tCore.buySwapFees = test.swapFees
		tCore.buyRedeemFees = test.redeemFees

		bh, err := newBalanceHandler([]*BotConfig{test.cfg}, tCore, tLogger)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		mkt := dcrBtcID
		if test.cfg.BaseAsset != 42 {
			mkt = ethBtcID
		}
		segregatedCore := bh.wrappedCoreForBot(mkt)
		res, err := segregatedCore.MaxBuy("host1", test.cfg.BaseAsset, test.cfg.QuoteAsset, test.rate)
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

func assetBalancesMatch(expected map[uint32]uint64, botName string, bh *balanceHandler) error {
	for assetID, exp := range expected {
		actual := bh.botBalance(botName, assetID)
		if actual != exp {
			return fmt.Errorf("asset %d expected %d != actual %d\n", assetID, exp, actual)
		}
	}
	return nil
}

func TestSegregatedCoreTrade(t *testing.T) {
	t.Run("single trade", func(t *testing.T) {
		testSegregatedCoreTrade(t, false)
	})
	t.Run("multi trade", func(t *testing.T) {
		testSegregatedCoreTrade(t, true)
	})
}

func testSegregatedCoreTrade(t *testing.T, testMultiTrade bool) {
	dcrBtcID := fmt.Sprintf("%s-%d-%d", "host1", 42, 0)

	id := encode.RandomBytes(order.OrderIDSize)
	id2 := encode.RandomBytes(order.OrderIDSize)

	matchIDs := make([]order.MatchID, 5)
	for i := range matchIDs {
		var matchID order.MatchID
		copy(matchID[:], encode.RandomBytes(order.MatchIDSize))
		matchIDs[i] = matchID
	}

	type noteAndBalances struct {
		note    core.Notification
		balance map[uint32]uint64
	}

	type test struct {
		name           string
		multiTradeOnly bool

		cfg               *BotConfig
		multiTrade        *core.MultiTradeForm
		trade             *core.TradeForm
		assetBalances     map[uint32]uint64
		postTradeBalances map[uint32]uint64
		market            *core.Market
		swapFees          uint64
		redeemFees        uint64
		tradeRes          *core.Order
		multiTradeRes     []*core.Order
		notifications     []*noteAndBalances
		isAccountLocker   map[uint32]bool
		maxFundingFees    uint64

		wantErr bool
	}

	tests := []test{
		// "cancelled order, 1/2 lots filled, sell"
		{
			name: "cancelled order, 1/2 lots filled, sell",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Percentage,
				BaseBalance:      50,
				QuoteBalanceType: Percentage,
				QuoteBalance:     50,
			},
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			trade: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    true,
				Qty:     2e6,
				Rate:    5e7,
			},
			market: &core.Market{
				LotSize: 1e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			tradeRes: &core.Order{
				ID:              id,
				LockedAmt:       2e6 + 2000,
				RedeemLockedAmt: 2000,
				Sell:            true,
			},
			postTradeBalances: map[uint32]uint64{
				0:  (1e7 / 2) - 2000,
				42: (1e7 / 2) - 2e6 - 2000,
			},
			notifications: []*noteAndBalances{
				{
					note: &core.OrderNote{
						Order: &core.Order{
							ID:      id,
							Status:  order.OrderStatusBooked,
							BaseID:  42,
							QuoteID: 0,
							Qty:     2e6,
							Sell:    true,
							Filled:  1e6,
							Matches: []*core.Match{
								{
									MatchID: matchIDs[0][:],
									Qty:     1e6,
									Rate:    5e7,
									Status:  order.MakerSwapCast,
								},
							},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 2000,
						42: (1e7 / 2) - 2e6 - 2000,
					},
				},
				{
					note: &core.MatchNote{
						OrderID: id,
						Match: &core.Match{
							MatchID: matchIDs[0][:],
							Qty:     1e6,
							Rate:    5e7,
							Status:  order.MatchComplete,
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 2000,
						42: (1e7 / 2) - 2e6 - 2000,
					},
				},
				{
					note: &core.MatchNote{
						OrderID: id,
						Match: &core.Match{
							MatchID: matchIDs[0][:],
							Qty:     1e6,
							Rate:    5e7,
							Status:  order.MatchConfirmed,
							Redeem:  &core.Coin{},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 2000 + calc.BaseToQuote(5e7, 1e6),
						42: (1e7 / 2) - 2e6 - 2000,
					},
				},
				{
					note: &core.OrderNote{
						Order: &core.Order{
							ID:               id,
							Status:           order.OrderStatusCanceled,
							BaseID:           42,
							QuoteID:          0,
							Qty:              2e6,
							Sell:             true,
							Filled:           2e6,
							AllFeesConfirmed: true,
							FeesPaid: &core.FeeBreakdown{
								Swap:       800,
								Redemption: 800,
							},
							Matches: []*core.Match{
								{
									MatchID: matchIDs[0][:],
									Qty:     1e6,
									Rate:    5e7,
									Status:  order.MakerSwapCast,
								},
							},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 800 + calc.BaseToQuote(5e7, 1e6),
						42: (1e7 / 2) - 1e6 - 800,
					},
				},
			},
		},
		// "cancelled order, 1/2 lots filled, buy"
		{
			name: "cancelled order, 1/2 lots filled, buy",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Percentage,
				BaseBalance:      50,
				QuoteBalanceType: Percentage,
				QuoteBalance:     50,
			},
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			trade: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    false,
				Qty:     2e6,
				Rate:    5e7,
			},
			market: &core.Market{
				LotSize: 1e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			tradeRes: &core.Order{
				ID:              id,
				LockedAmt:       calc.BaseToQuote(5e7, 2e6) + 2000,
				RedeemLockedAmt: 2000,
				Sell:            false,
			},
			postTradeBalances: map[uint32]uint64{
				0:  (1e7 / 2) - 2000 - calc.BaseToQuote(5e7, 2e6),
				42: (1e7 / 2) - 2000,
			},
			notifications: []*noteAndBalances{
				{
					note: &core.OrderNote{
						Order: &core.Order{
							ID:      id,
							Status:  order.OrderStatusBooked,
							BaseID:  42,
							QuoteID: 0,
							Qty:     2e6,
							Sell:    false,
							Filled:  1e6,
							Matches: []*core.Match{
								{
									MatchID: matchIDs[0][:],
									Qty:     1e6,
									Rate:    5e7,
									Status:  order.MakerSwapCast,
								},
							},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 2000 - calc.BaseToQuote(5e7, 2e6),
						42: (1e7 / 2) - 2000,
					},
				},
				{
					note: &core.MatchNote{
						OrderID: id,
						Match: &core.Match{
							MatchID: matchIDs[0][:],
							Qty:     1e6,
							Rate:    5e7,
							Status:  order.MatchComplete,
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 2000 - calc.BaseToQuote(5e7, 2e6),
						42: (1e7 / 2) - 2000,
					},
				},
				{
					note: &core.MatchNote{
						OrderID: id,
						Match: &core.Match{
							MatchID: matchIDs[0][:],
							Qty:     1e6,
							Rate:    5e7,
							Status:  order.MatchConfirmed,
							Redeem:  &core.Coin{},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 2000 - calc.BaseToQuote(5e7, 2e6),
						42: (1e7 / 2) - 2000 + 1e6,
					},
				},
				{
					note: &core.OrderNote{
						Order: &core.Order{
							ID:               id,
							Status:           order.OrderStatusCanceled,
							BaseID:           42,
							QuoteID:          0,
							Qty:              2e6,
							Sell:             false,
							Filled:           2e6,
							AllFeesConfirmed: true,
							Rate:             5e7,
							FeesPaid: &core.FeeBreakdown{
								Swap:       800,
								Redemption: 800,
							},
							Matches: []*core.Match{
								{
									MatchID: matchIDs[0][:],
									Qty:     1e6,
									Rate:    5e7,
									Status:  order.MatchConfirmed,
								},
							},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 800 - calc.BaseToQuote(5e7, 1e6),
						42: (1e7 / 2) - 800 + 1e6,
					},
				},
			},
		},
		// "fully filled order, sell"
		{
			name: "fully filled order, sell",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Percentage,
				BaseBalance:      50,
				QuoteBalanceType: Percentage,
				QuoteBalance:     50,
			},
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			trade: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    true,
				Qty:     2e6,
				Rate:    5e7,
			},
			market: &core.Market{
				LotSize: 1e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			tradeRes: &core.Order{
				ID:              id,
				LockedAmt:       2e6 + 2000,
				RedeemLockedAmt: 2000,
				Sell:            true,
			},
			postTradeBalances: map[uint32]uint64{
				0:  (1e7 / 2) - 2000,
				42: (1e7 / 2) - 2e6 - 2000,
			},
			notifications: []*noteAndBalances{
				{
					note: &core.OrderNote{
						Order: &core.Order{
							ID:      id,
							Status:  order.OrderStatusBooked,
							BaseID:  42,
							QuoteID: 0,
							Qty:     2e6,
							Sell:    true,
							Filled:  1e6,
							Matches: []*core.Match{
								{
									MatchID: matchIDs[0][:],
									Qty:     1e6,
									Rate:    5e7,
									Status:  order.MakerSwapCast,
								},
							},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 2000,
						42: (1e7 / 2) - 2e6 - 2000,
					},
				},
				{
					note: &core.MatchNote{
						OrderID: id,
						Match: &core.Match{
							MatchID: matchIDs[0][:],
							Qty:     1e6,
							Rate:    5e7,
							Status:  order.MatchComplete,
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 2000,
						42: (1e7 / 2) - 2e6 - 2000,
					},
				},
				{
					note: &core.MatchNote{
						OrderID: id,
						Match: &core.Match{
							MatchID: matchIDs[0][:],
							Qty:     1e6,
							Rate:    5e7,
							Status:  order.MatchConfirmed,
							Redeem:  &core.Coin{},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 2000 + calc.BaseToQuote(5e7, 1e6),
						42: (1e7 / 2) - 2e6 - 2000,
					},
				},
				{
					note: &core.OrderNote{
						Order: &core.Order{
							ID:               id,
							Status:           order.OrderStatusExecuted,
							BaseID:           42,
							QuoteID:          0,
							Qty:              2e6,
							Sell:             true,
							Filled:           2e6,
							AllFeesConfirmed: true,
							FeesPaid: &core.FeeBreakdown{
								Swap:       1600,
								Redemption: 1600,
							},
							Matches: []*core.Match{
								{
									MatchID: matchIDs[0][:],
									Qty:     1e6,
									Rate:    5e7,
									Status:  order.MatchConfirmed,
								},
								{
									MatchID: matchIDs[1][:],
									Qty:     1e6,
									Rate:    55e6,
									Status:  order.MakerSwapCast,
								},
							},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 1600 + calc.BaseToQuote(5e7, 1e6),
						42: (1e7 / 2) - 2e6 - 1600,
					},
				},
				{
					note: &core.MatchNote{
						OrderID: id,
						Match: &core.Match{
							MatchID: matchIDs[1][:],
							Qty:     1e6,
							Rate:    55e6,
							Status:  order.MatchComplete,
							Redeem:  &core.Coin{},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 1600 + calc.BaseToQuote(5e7, 1e6),
						42: (1e7 / 2) - 2e6 - 1600,
					},
				},
				{
					note: &core.MatchNote{
						OrderID: id,
						Match: &core.Match{
							MatchID: matchIDs[1][:],
							Qty:     1e6,
							Rate:    55e6,
							Status:  order.MatchConfirmed,
							Redeem:  &core.Coin{},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 1600 + calc.BaseToQuote(525e5, 2e6),
						42: (1e7 / 2) - 2e6 - 1600,
					},
				},
			},
		},
		// "fully filled order, buy"
		{
			name: "fully filled order, buy",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Percentage,
				BaseBalance:      50,
				QuoteBalanceType: Percentage,
				QuoteBalance:     50,
			},
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			trade: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    false,
				Qty:     2e6,
				Rate:    5e7,
			},
			market: &core.Market{
				LotSize: 1e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			tradeRes: &core.Order{
				ID:              id,
				LockedAmt:       calc.BaseToQuote(5e7, 2e6) + 2000,
				RedeemLockedAmt: 2000,
				Sell:            true,
			},
			postTradeBalances: map[uint32]uint64{
				0:  (1e7 / 2) - 2000 - calc.BaseToQuote(5e7, 2e6),
				42: (1e7 / 2) - 2000,
			},
			notifications: []*noteAndBalances{
				{
					note: &core.OrderNote{
						Order: &core.Order{
							ID:      id,
							Status:  order.OrderStatusBooked,
							BaseID:  42,
							QuoteID: 0,
							Qty:     2e6,
							Sell:    false,
							Filled:  1e6,
							Matches: []*core.Match{
								{
									MatchID: matchIDs[0][:],
									Qty:     1e6,
									Rate:    5e7,
									Status:  order.MakerSwapCast,
								},
							},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 2000 - calc.BaseToQuote(5e7, 2e6),
						42: (1e7 / 2) - 2000,
					},
				},
				{
					note: &core.MatchNote{
						OrderID: id,
						Match: &core.Match{
							MatchID: matchIDs[0][:],
							Qty:     1e6,
							Rate:    5e7,
							Status:  order.MatchComplete,
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 2000 - calc.BaseToQuote(5e7, 2e6),
						42: (1e7 / 2) - 2000,
					},
				},
				{
					note: &core.MatchNote{
						OrderID: id,
						Match: &core.Match{
							MatchID: matchIDs[0][:],
							Qty:     1e6,
							Rate:    5e7,
							Status:  order.MatchConfirmed,
							Redeem:  &core.Coin{},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 2000 - calc.BaseToQuote(5e7, 2e6),
						42: (1e7 / 2) - 2000 + 1e6,
					},
				},
				{
					note: &core.OrderNote{
						Order: &core.Order{
							ID:               id,
							Status:           order.OrderStatusExecuted,
							BaseID:           42,
							QuoteID:          0,
							Qty:              2e6,
							Rate:             5e7,
							Sell:             false,
							Filled:           2e6,
							AllFeesConfirmed: true,
							FeesPaid: &core.FeeBreakdown{
								Swap:       1600,
								Redemption: 1600,
							},
							Matches: []*core.Match{
								{
									MatchID: matchIDs[0][:],
									Qty:     1e6,
									Rate:    5e7,
									Status:  order.MatchConfirmed,
								},
								{
									MatchID: matchIDs[1][:],
									Qty:     1e6,
									Rate:    45e6,
									Status:  order.MakerSwapCast,
								},
							},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 1600 - calc.BaseToQuote(5e7, 1e6) - calc.BaseToQuote(45e6, 1e6),
						42: (1e7 / 2) - 1600 + 1e6,
					},
				},
				{
					note: &core.MatchNote{
						OrderID: id,
						Match: &core.Match{
							MatchID: matchIDs[1][:],
							Qty:     1e6,
							Rate:    45e6,
							Status:  order.MatchComplete,
							Redeem:  &core.Coin{},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 1600 - calc.BaseToQuote(5e7, 1e6) - calc.BaseToQuote(45e6, 1e6),
						42: (1e7 / 2) - 1600 + 1e6,
					},
				},
				{
					note: &core.MatchNote{
						OrderID: id,
						Match: &core.Match{
							MatchID: matchIDs[1][:],
							Qty:     1e6,
							Rate:    45e6,
							Status:  order.MatchConfirmed,
							Redeem:  &core.Coin{},
						},
					},
					balance: map[uint32]uint64{
						0:  (1e7 / 2) - 1600 - calc.BaseToQuote(5e7, 1e6) - calc.BaseToQuote(45e6, 1e6),
						42: (1e7 / 2) - 1600 + 2e6,
					},
				},
			},
		},
		// "edge enough balance for single buy"
		{
			name: "edge enough balance for single buy",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      5e6,
				QuoteBalanceType: Amount,
				QuoteBalance:     calc.BaseToQuote(5e7, 5e6) + 1500,
			},
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			trade: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    false,
				Qty:     5e6,
				Rate:    5e7,
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:       1000,
			redeemFees:     1000,
			maxFundingFees: 500,
			tradeRes: &core.Order{
				ID:              id,
				LockedAmt:       calc.BaseToQuote(5e7, 5e6) + 1000,
				RedeemLockedAmt: 0,
				Sell:            true,
				FeesPaid: &core.FeeBreakdown{
					Funding: 400,
				},
			},
			postTradeBalances: map[uint32]uint64{
				0:  100,
				42: 5e6,
			},
		},
		// "edge not enough balance for single buy, with maxFundingFee > 0"
		{
			name: "edge not enough balance for single buy",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      5e6,
				QuoteBalanceType: Amount,
				QuoteBalance:     calc.BaseToQuote(5e7, 5e6) + 1499,
			},
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			trade: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    false,
				Qty:     5e6,
				Rate:    5e7,
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:       1000,
			redeemFees:     1000,
			maxFundingFees: 500,
			wantErr:        true,
		},
		// "edge enough balance for single sell"
		{
			name: "edge enough balance for single sell",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      5e6 + 1500,
				QuoteBalanceType: Amount,
				QuoteBalance:     5e6,
			},
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			trade: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    true,
				Qty:     5e6,
				Rate:    1e8,
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:       1000,
			redeemFees:     1000,
			maxFundingFees: 500,
			tradeRes: &core.Order{
				ID:              id,
				LockedAmt:       5e6 + 1000,
				RedeemLockedAmt: 0,
				Sell:            true,
				FeesPaid: &core.FeeBreakdown{
					Funding: 400,
				},
			},
			postTradeBalances: map[uint32]uint64{
				0:  5e6,
				42: 100,
			},
		},
		// "edge not enough balance for single sell"
		{
			name: "edge not enough balance for single sell",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      5e6 + 1499,
				QuoteBalanceType: Amount,
				QuoteBalance:     5e6,
			},
			assetBalances: map[uint32]uint64{
				0:  1e7,
				42: 1e7,
			},
			trade: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Sell:    true,
				Qty:     5e6,
				Rate:    1e8,
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:       1000,
			redeemFees:     1000,
			maxFundingFees: 500,
			wantErr:        true,
		},
		// "edge enough balance for single buy with redeem fees"
		{
			name: "edge enough balance for single buy with redeem fees",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      1000,
				QuoteBalanceType: Amount,
				QuoteBalance:     calc.BaseToQuote(52e7, 5e6) + 1000,
			},
			assetBalances: map[uint32]uint64{
				0:  1e8,
				42: 1e7,
			},
			trade: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Qty:     5e6,
				Rate:    52e7,
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			tradeRes: &core.Order{
				ID:              id,
				LockedAmt:       calc.BaseToQuote(52e7, 5e6) + 1000,
				RedeemLockedAmt: 1000,
			},
			postTradeBalances: map[uint32]uint64{
				0:  0,
				42: 0,
			},
			isAccountLocker: map[uint32]bool{42: true},
		},
		// "edge not enough balance for single buy due to redeem fees"
		{
			name: "edge not enough balance for single buy due to redeem fees",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      999,
				QuoteBalanceType: Amount,
				QuoteBalance:     calc.BaseToQuote(52e7, 5e6) + 1000,
			},
			assetBalances: map[uint32]uint64{
				0:  1e8,
				42: 1e7,
			},
			trade: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Base:    42,
				Quote:   0,
				Qty:     5e6,
				Rate:    52e7,
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:        1000,
			redeemFees:      1000,
			isAccountLocker: map[uint32]bool{42: true},
			wantErr:         true,
		},
		// "edge enough balance for single sell with redeem fees"
		{
			name: "edge enough balance for single sell with redeem fees",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      5e6 + 1000,
				QuoteBalanceType: Amount,
				QuoteBalance:     1000,
			},
			assetBalances: map[uint32]uint64{
				0:  1e8,
				42: 1e7,
			},
			trade: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Sell:    true,
				Base:    42,
				Quote:   0,
				Qty:     5e6,
				Rate:    52e7,
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			tradeRes: &core.Order{
				ID:              id,
				LockedAmt:       5e6 + 1000,
				RedeemLockedAmt: 1000,
			},
			postTradeBalances: map[uint32]uint64{
				0:  0,
				42: 0,
			},
			isAccountLocker: map[uint32]bool{0: true},
		},
		// "edge not enough balance for single buy due to redeem fees"
		{
			name: "edge not enough balance for single sell due to redeem fees",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      5e6 + 1000,
				QuoteBalanceType: Amount,
				QuoteBalance:     999,
			},
			assetBalances: map[uint32]uint64{
				0:  1e8,
				42: 1e7,
			},
			trade: &core.TradeForm{
				Host:    "host1",
				IsLimit: true,
				Sell:    true,
				Base:    42,
				Quote:   0,
				Qty:     5e6,
				Rate:    52e7,
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:        1000,
			redeemFees:      1000,
			isAccountLocker: map[uint32]bool{0: true},
			wantErr:         true,
		},
		// "edge enough balance for multi buy"
		{
			name: "edge enough balance for multi buy",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      5e6,
				QuoteBalanceType: Amount,
				QuoteBalance:     calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(52e7, 5e6) + 2500,
			},
			assetBalances: map[uint32]uint64{
				0:  1e8,
				42: 1e7,
			},
			multiTradeOnly: true,
			multiTrade: &core.MultiTradeForm{
				Host:  "host1",
				Base:  42,
				Quote: 0,
				Placements: []*core.QtyRate{
					{
						Qty:  5e6,
						Rate: 52e7,
					},
					{
						Qty:  5e6,
						Rate: 5e7,
					},
				},
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:       1000,
			redeemFees:     1000,
			maxFundingFees: 500,
			multiTradeRes: []*core.Order{{
				ID:              id,
				LockedAmt:       calc.BaseToQuote(5e7, 5e6) + 1000,
				RedeemLockedAmt: 0,
				Sell:            true,
				FeesPaid: &core.FeeBreakdown{
					Funding: 400,
				},
			}, {
				ID:              id2,
				LockedAmt:       calc.BaseToQuote(52e7, 5e6) + 1000,
				RedeemLockedAmt: 0,
				Sell:            true,
			},
			},
			postTradeBalances: map[uint32]uint64{
				0:  100,
				42: 5e6,
			},
		},
		// "edge not enough balance for multi buy"
		{
			name: "edge not enough balance for multi buy",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      5e6,
				QuoteBalanceType: Amount,
				QuoteBalance:     calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(52e7, 5e6) + 2499,
			},
			assetBalances: map[uint32]uint64{
				0:  1e8,
				42: 1e7,
			},
			multiTradeOnly: true,
			multiTrade: &core.MultiTradeForm{
				Host:  "host1",
				Base:  42,
				Quote: 0,
				Placements: []*core.QtyRate{
					{
						Qty:  5e6,
						Rate: 52e7,
					},
					{
						Qty:  5e6,
						Rate: 5e7,
					},
				},
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:       1000,
			redeemFees:     1000,
			maxFundingFees: 500,
			wantErr:        true,
		},
		// "edge enough balance for multi sell"
		{
			name: "edge enough balance for multi sell",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      1e7 + 2500,
				QuoteBalanceType: Amount,
				QuoteBalance:     5e6,
			},
			assetBalances: map[uint32]uint64{
				0:  1e8,
				42: 1e8,
			},
			multiTradeOnly: true,
			multiTrade: &core.MultiTradeForm{
				Host:  "host1",
				Base:  42,
				Quote: 0,
				Sell:  true,
				Placements: []*core.QtyRate{
					{
						Qty:  5e6,
						Rate: 52e7,
					},
					{
						Qty:  5e6,
						Rate: 5e7,
					},
				},
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:       1000,
			redeemFees:     1000,
			maxFundingFees: 500,
			multiTradeRes: []*core.Order{{
				ID:              id,
				LockedAmt:       5e6 + 1000,
				RedeemLockedAmt: 0,
				Sell:            true,
				FeesPaid: &core.FeeBreakdown{
					Funding: 400,
				},
			}, {
				ID:              id2,
				LockedAmt:       5e6 + 1000,
				RedeemLockedAmt: 0,
				Sell:            true,
			},
			},
			postTradeBalances: map[uint32]uint64{
				0:  5e6,
				42: 100,
			},
		},
		// "edge not enough balance for multi sell"
		{
			name: "edge not enough balance for multi sell",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      1e7 + 2499,
				QuoteBalanceType: Amount,
				QuoteBalance:     5e6,
			},
			assetBalances: map[uint32]uint64{
				0:  1e8,
				42: 1e8,
			},
			multiTradeOnly: true,
			multiTrade: &core.MultiTradeForm{
				Host:  "host1",
				Base:  42,
				Quote: 0,
				Sell:  true,
				Placements: []*core.QtyRate{
					{
						Qty:  5e6,
						Rate: 52e7,
					},
					{
						Qty:  5e6,
						Rate: 5e7,
					},
				},
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:       1000,
			redeemFees:     1000,
			maxFundingFees: 500,
			wantErr:        true,
		},
		// "edge enough balance for multi buy with redeem fees"
		{
			name: "edge enough balance for multi buy with redeem fees",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      2000,
				QuoteBalanceType: Amount,
				QuoteBalance:     calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(52e7, 5e6) + 2000,
			},
			assetBalances: map[uint32]uint64{
				0:  1e8,
				42: 1e7,
			},
			multiTradeOnly: true,
			multiTrade: &core.MultiTradeForm{
				Host:  "host1",
				Base:  42,
				Quote: 0,
				Placements: []*core.QtyRate{
					{
						Qty:  5e6,
						Rate: 52e7,
					},
					{
						Qty:  5e6,
						Rate: 5e7,
					},
				},
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			multiTradeRes: []*core.Order{{
				ID:              id,
				LockedAmt:       calc.BaseToQuote(5e7, 5e6) + 1000,
				RedeemLockedAmt: 1000,
				Sell:            true,
			}, {
				ID:              id2,
				LockedAmt:       calc.BaseToQuote(52e7, 5e6) + 1000,
				RedeemLockedAmt: 1000,
				Sell:            true,
			},
			},
			postTradeBalances: map[uint32]uint64{
				0:  0,
				42: 0,
			},
			isAccountLocker: map[uint32]bool{42: true},
		},
		// "edge not enough balance for multi buy due to redeem fees"
		{
			name: "edge not enough balance for multi buy due to redeem fees",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      1999,
				QuoteBalanceType: Amount,
				QuoteBalance:     calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(52e7, 5e6) + 2000,
			},
			assetBalances: map[uint32]uint64{
				0:  1e8,
				42: 1e7,
			},
			multiTradeOnly: true,
			multiTrade: &core.MultiTradeForm{
				Host:  "host1",
				Base:  42,
				Quote: 0,
				Placements: []*core.QtyRate{
					{
						Qty:  5e6,
						Rate: 52e7,
					},
					{
						Qty:  5e6,
						Rate: 5e7,
					},
				},
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:        1000,
			redeemFees:      1000,
			wantErr:         true,
			isAccountLocker: map[uint32]bool{42: true},
		},
		// "edge enough balance for multi sell with redeem fees"
		{
			name: "edge enough balance for multi sell with redeem fees",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      1e7 + 2000,
				QuoteBalanceType: Amount,
				QuoteBalance:     2000,
			},
			assetBalances: map[uint32]uint64{
				0:  1e8,
				42: 1e8,
			},
			multiTradeOnly: true,
			multiTrade: &core.MultiTradeForm{
				Host:  "host1",
				Base:  42,
				Quote: 0,
				Sell:  true,
				Placements: []*core.QtyRate{
					{
						Qty:  5e6,
						Rate: 52e7,
					},
					{
						Qty:  5e6,
						Rate: 5e7,
					},
				},
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:   1000,
			redeemFees: 1000,
			multiTradeRes: []*core.Order{{
				ID:              id,
				LockedAmt:       5e6 + 1000,
				RedeemLockedAmt: 1000,
				Sell:            true,
			}, {
				ID:              id2,
				LockedAmt:       5e6 + 1000,
				RedeemLockedAmt: 1000,
				Sell:            true,
			},
			},
			isAccountLocker: map[uint32]bool{0: true},
			postTradeBalances: map[uint32]uint64{
				0:  0,
				42: 0,
			},
		},
		// "edge not enough balance for multi sell due to redeem fees"
		{
			name: "edge enough balance for multi sell with redeem fees",
			cfg: &BotConfig{
				Host:             "host1",
				BaseAsset:        42,
				QuoteAsset:       0,
				BaseBalanceType:  Amount,
				BaseBalance:      1e7 + 2000,
				QuoteBalanceType: Amount,
				QuoteBalance:     1999,
			},
			assetBalances: map[uint32]uint64{
				0:  1e8,
				42: 1e8,
			},
			multiTradeOnly: true,
			multiTrade: &core.MultiTradeForm{
				Host:  "host1",
				Base:  42,
				Quote: 0,
				Sell:  true,
				Placements: []*core.QtyRate{
					{
						Qty:  5e6,
						Rate: 52e7,
					},
					{
						Qty:  5e6,
						Rate: 5e7,
					},
				},
			},
			market: &core.Market{
				LotSize: 5e6,
			},
			swapFees:        1000,
			redeemFees:      1000,
			isAccountLocker: map[uint32]bool{0: true},
			wantErr:         true,
		},
	}

	runTest := func(test *test) {
		if test.multiTradeOnly && !testMultiTrade {
			return
		}

		fmt.Println(test.name)

		tCore := newTCore()
		tCore.setAssetBalances(test.assetBalances)
		tCore.market = test.market
		if !test.multiTradeOnly {
			if test.trade.Sell {
				tCore.sellSwapFees = test.swapFees
				tCore.sellRedeemFees = test.redeemFees
			} else {
				tCore.buySwapFees = test.swapFees
				tCore.buyRedeemFees = test.redeemFees
			}
		} else {
			if test.multiTrade.Sell {
				tCore.sellSwapFees = test.swapFees
				tCore.sellRedeemFees = test.redeemFees
			} else {
				tCore.buySwapFees = test.swapFees
				tCore.buyRedeemFees = test.redeemFees
			}
		}
		if test.isAccountLocker == nil {
			tCore.isAccountLocker = make(map[uint32]bool)
		} else {
			tCore.isAccountLocker = test.isAccountLocker
		}
		tCore.maxFundingFees = test.maxFundingFees

		if testMultiTrade {
			if test.multiTradeOnly {
				tCore.multiTradeResult = test.multiTradeRes
			} else {
				tCore.multiTradeResult = []*core.Order{test.tradeRes}
			}
		} else {
			tCore.tradeResult = test.tradeRes
		}
		tCore.noteFeed = make(chan core.Notification)

		bh, err := newBalanceHandler([]*BotConfig{test.cfg}, tCore, tLogger)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go bh.run(ctx)

		segregatedCore := bh.wrappedCoreForBot(dcrBtcID)

		if testMultiTrade {

			if test.multiTradeOnly {
				_, err = segregatedCore.MultiTrade([]byte{}, test.multiTrade)
			} else {
				_, err = segregatedCore.MultiTrade([]byte{}, &core.MultiTradeForm{
					Host:  test.trade.Host,
					Sell:  test.trade.Sell,
					Base:  test.trade.Base,
					Quote: test.trade.Quote,
					Placements: []*core.QtyRate{
						{
							Qty:  test.trade.Qty,
							Rate: test.trade.Rate,
						},
					},
					Options: test.trade.Options,
				})
			}
		} else {
			_, err = segregatedCore.Trade([]byte{}, test.trade)
		}
		if test.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error but did not get", test.name)
			}
			return
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		if err := assetBalancesMatch(test.postTradeBalances, dcrBtcID, bh); err != nil {
			t.Fatalf("%s: unexpected post trade balance: %v", test.name, err)
		}

		dummyNote := &core.BondRefundNote{}
		for i, noteAndBalances := range test.notifications {
			tCore.noteFeed <- noteAndBalances.note
			tCore.noteFeed <- dummyNote

			if err := assetBalancesMatch(noteAndBalances.balance, dcrBtcID, bh); err != nil {
				t.Fatalf("%s: unexpected balances after note %d: %v", test.name, i, err)
			}
		}
	}

	for _, test := range tests {
		runTest(&test)
	}
}
