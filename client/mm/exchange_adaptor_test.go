package mm

import (
	"context"
	"encoding/hex"
	"fmt"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/client/asset"
	"decred.org/dcrdex/client/core"
	"decred.org/dcrdex/client/mm/libxc"
	"decred.org/dcrdex/dex"
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
	coinIDs := make([]string, 6)
	for i := range coinIDs {
		coinIDs[i] = hex.EncodeToString(encode.RandomBytes(32))
	}

	type matchUpdate struct {
		swapCoin   *dex.Bytes
		redeemCoin *dex.Bytes
		refundCoin *dex.Bytes
	}
	newMatchUpdate := func(swapCoin, redeemCoin, refundCoin *string) *matchUpdate {
		stringToBytes := func(s *string) *dex.Bytes {
			if s == nil {
				return nil
			}
			b, _ := hex.DecodeString(*s)
			d := dex.Bytes(b)
			return &d
		}

		return &matchUpdate{
			swapCoin:   stringToBytes(swapCoin),
			redeemCoin: stringToBytes(redeemCoin),
			refundCoin: stringToBytes(refundCoin),
		}
	}

	type orderUpdate struct {
		id                   order.OrderID
		lockedAmt            uint64
		parentAssetLockedAmt uint64
		redeemLockedAmt      uint64
		refundLockedAmt      uint64
		status               order.OrderStatus
		matches              []*matchUpdate
		allFeesConfirmed     bool
	}
	newOrderUpdate := func(id order.OrderID, lockedAmt, parentAssetLockedAmt, redeemLockedAmt, refundLockedAmt uint64, status order.OrderStatus, allFeesConfirmed bool, matches ...*matchUpdate) *orderUpdate {
		return &orderUpdate{
			id:                   id,
			lockedAmt:            lockedAmt,
			parentAssetLockedAmt: parentAssetLockedAmt,
			redeemLockedAmt:      redeemLockedAmt,
			refundLockedAmt:      refundLockedAmt,
			status:               status,
			matches:              matches,
			allFeesConfirmed:     allFeesConfirmed,
		}
	}

	type orderLockedFunds struct {
		id                   order.OrderID
		lockedAmt            uint64
		parentAssetLockedAmt uint64
		redeemLockedAmt      uint64
		refundLockedAmt      uint64
	}
	newOrderLockedFunds := func(id order.OrderID, lockedAmt, parentAssetLockedAmt, redeemLockedAmt, refundLockedAmt uint64) *orderLockedFunds {
		return &orderLockedFunds{
			id:                   id,
			lockedAmt:            lockedAmt,
			parentAssetLockedAmt: parentAssetLockedAmt,
			redeemLockedAmt:      redeemLockedAmt,
			refundLockedAmt:      refundLockedAmt,
		}
	}

	newWalletTx := func(id string, txType asset.TransactionType, amount, fees uint64, confirmed bool) *asset.WalletTransaction {
		return &asset.WalletTransaction{
			ID:        id,
			Amount:    amount,
			Fees:      fees,
			Confirmed: confirmed,
			Type:      txType,
		}
	}

	b2q := calc.BaseToQuote

	type updatesAndBalances struct {
		orderUpdate      *orderUpdate
		txUpdates        []*asset.WalletTransaction
		balances         map[uint32]*botBalance
		numPendingTrades int
	}

	type test struct {
		name               string
		isDynamicSwapper   map[uint32]bool
		initialBalances    map[uint32]uint64
		multiTrade         *core.MultiTradeForm
		initialLockedFunds []*orderLockedFunds

		wantErr            bool
		postTradeBalances  map[uint32]*botBalance
		updatesAndBalances []*updatesAndBalances
	}

	tests := []*test{
		{
			name: "non dynamic swapper, sell",
			initialBalances: map[uint32]uint64{
				42: 1e8,
				0:  1e8,
			},
			multiTrade: &core.MultiTradeForm{
				Host:  host,
				Sell:  true,
				Base:  42,
				Quote: 0,
				Placements: []*core.QtyRate{
					{Qty: 5e6, Rate: 5e7},
					{Qty: 5e6, Rate: 6e7},
				},
			},
			initialLockedFunds: []*orderLockedFunds{
				newOrderLockedFunds(orderIDs[0], 5e6+2000, 0, 0, 0),
				newOrderLockedFunds(orderIDs[1], 5e6+2000, 0, 0, 0),
			},
			postTradeBalances: map[uint32]*botBalance{
				42: {1e8 - (5e6+2000)*2, (5e6 + 2000) * 2, 0},
				0:  {1e8, 0, 0},
			},
			updatesAndBalances: []*updatesAndBalances{
				// First order has a match and sends a swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2e6, 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3e6+1000, 0, 0, 0, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[0], nil, nil)),
					balances: map[uint32]*botBalance{
						42: {1e8 - (5e6+2000)*2, 5e6 + 2000 + 3e6 + 1000, 0},
						0:  {1e8, 0, 0},
					},
					numPendingTrades: 2,
				},
				// Second order has a match and sends swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3e6, 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 2e6+1000, 0, 0, 0, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[1], nil, nil)),
					balances: map[uint32]*botBalance{
						42: {1e8 - (5e6+2000)*2, 5e6 + 2000, 0},
						0:  {1e8, 0, 0},
					},
					numPendingTrades: 2,
				},
				// First order swap is confirmed, and redemption is sent
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2e6, 1000, true),
						newWalletTx(coinIDs[2], asset.Redeem, b2q(5e7, 2e6), 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3e6+1000, 0, 0, 0, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[0], &coinIDs[2], nil)),
					balances: map[uint32]*botBalance{
						42: {1e8 - (5e6+2000)*2, 5e6 + 2000, 0},
						0:  {1e8, 0, b2q(5e7, 2e6) - 1000},
					},
					numPendingTrades: 2,
				},
				// First order redemption confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[2], asset.Redeem, b2q(5e7, 2e6), 1000, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3e6+1000, 0, 0, 0, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[0], &coinIDs[2], nil)),
					balances: map[uint32]*botBalance{
						42: {1e8 - (5e6+2000)*2, 5e6 + 2000, 0},
						0:  {1e8 + b2q(5e7, 2e6) - 1000, 0, 0},
					},
					numPendingTrades: 2,
				},
				// First order cancelled
				{
					orderUpdate: newOrderUpdate(orderIDs[0], 0, 0, 0, 0, order.OrderStatusCanceled, true, newMatchUpdate(&coinIDs[0], &coinIDs[2], nil)),
					balances: map[uint32]*botBalance{
						42: {1e8 - (7e6 + 2000 + 1000), 2e6 + 1000, 0},
						0:  {1e8 + b2q(5e7, 2e6) - 1000, 0, 0},
					},
					numPendingTrades: 1,
				},
				// Second order second match, swap sent, and first match refunded
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3e6, 1000, true),
						newWalletTx(coinIDs[3], asset.Refund, 3e6, 1200, false),
						newWalletTx(coinIDs[4], asset.Swap, 2e6, 800, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, false, newMatchUpdate(&coinIDs[1], nil, &coinIDs[3]), newMatchUpdate(&coinIDs[4], nil, nil)),
					balances: map[uint32]*botBalance{
						42: {1e8 - (7e6 + 1800 + 1000), 0, 3e6 - 1200},
						0:  {1e8 + b2q(5e7, 2e6) - 1000, 0, 0},
					},
					numPendingTrades: 1,
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[3], asset.Refund, 3e6, 1200, true),
						newWalletTx(coinIDs[4], asset.Swap, 2e6, 800, true),
						newWalletTx(coinIDs[5], asset.Redeem, b2q(6e7, 2e6), 700, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, true, newMatchUpdate(&coinIDs[1], nil, &coinIDs[3]), newMatchUpdate(&coinIDs[4], &coinIDs[5], nil)),
					balances: map[uint32]*botBalance{
						42: {1e8 - (4e6 + 1800 + 1000 + 1200), 0, 0},
						0:  {1e8 + b2q(5e7, 2e6) + b2q(6e7, 2e6) - 1700, 0, 0},
					},
				},
			},
		},
		{
			name: "non dynamic swapper, buy",
			initialBalances: map[uint32]uint64{
				42: 1e8,
				0:  1e8,
			},
			multiTrade: &core.MultiTradeForm{
				Host:  host,
				Sell:  false,
				Base:  42,
				Quote: 0,
				Placements: []*core.QtyRate{
					{Qty: 5e6, Rate: 5e7},
					{Qty: 5e6, Rate: 6e7},
				},
			},
			initialLockedFunds: []*orderLockedFunds{
				newOrderLockedFunds(orderIDs[0], b2q(5e7, 5e6)+2000, 0, 0, 0),
				newOrderLockedFunds(orderIDs[1], b2q(6e7, 5e6)+2000, 0, 0, 0),
			},
			postTradeBalances: map[uint32]*botBalance{
				42: {1e8, 0, 0},
				0:  {1e8 - (b2q(5e7, 5e6) + b2q(6e7, 5e6) + 4000), b2q(5e7, 5e6) + b2q(6e7, 5e6) + 4000, 0},
			},
			updatesAndBalances: []*updatesAndBalances{
				// First order has a match and sends a swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, b2q(5e7, 2e6), 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], b2q(5e7, 3e6)+1000, 0, 0, 0, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[0], nil, nil)),
					balances: map[uint32]*botBalance{
						42: {1e8, 0, 0},
						0:  {1e8 - (b2q(5e7, 5e6) + b2q(6e7, 5e6) + 4000), b2q(5e7, 3e6) + b2q(6e7, 5e6) + 3000, 0},
					},
					numPendingTrades: 2,
				},
				// Second order has a match and sends swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, b2q(6e7, 3e6), 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], b2q(6e7, 2e6)+1000, 0, 0, 0, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[1], nil, nil)),
					balances: map[uint32]*botBalance{
						42: {1e8, 0, 0},
						0:  {1e8 - (b2q(5e7, 5e6) + b2q(6e7, 5e6) + 4000), b2q(5e7, 3e6) + b2q(6e7, 2e6) + 2000, 0},
					},
					numPendingTrades: 2,
				},
				// First order swap is confirmed, and redemption is sent
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, b2q(5e7, 2e6), 1000, true),
						newWalletTx(coinIDs[2], asset.Redeem, 2e6, 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], b2q(5e7, 3e6)+1000, 0, 0, 0, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[0], &coinIDs[2], nil)),
					balances: map[uint32]*botBalance{
						42: {1e8, 0, 2e6 - 1000},
						0:  {1e8 - (b2q(5e7, 5e6) + b2q(6e7, 5e6) + 4000), b2q(5e7, 3e6) + b2q(6e7, 2e6) + 2000, 0},
					},
					numPendingTrades: 2,
				},
				// First order redemption confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[2], asset.Redeem, 2e6, 1000, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], b2q(5e7, 3e6)+1000, 0, 0, 0, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[0], &coinIDs[2], nil)),
					balances: map[uint32]*botBalance{
						42: {1e8 + 2e6 - 1000, 0, 0},
						0:  {1e8 - (b2q(5e7, 5e6) + b2q(6e7, 5e6) + 4000), b2q(5e7, 3e6) + b2q(6e7, 2e6) + 2000, 0},
					},
					numPendingTrades: 2,
				},
				// First order cancelled
				{
					orderUpdate: newOrderUpdate(orderIDs[0], 0, 0, 0, 0, order.OrderStatusCanceled, true, newMatchUpdate(&coinIDs[0], &coinIDs[2], nil)),
					balances: map[uint32]*botBalance{
						42: {1e8 + 2e6 - 1000, 0, 0},
						0:  {1e8 - (b2q(5e7, 2e6) + b2q(6e7, 5e6) + 3000), b2q(6e7, 2e6) + 1000, 0},
					},
					numPendingTrades: 1,
				},
				// Second order second match, swap sent, and first match refunded
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, b2q(6e7, 3e6), 1000, true),
						newWalletTx(coinIDs[3], asset.Refund, b2q(6e7, 3e6), 1200, false),
						newWalletTx(coinIDs[4], asset.Swap, b2q(6e7, 2e6), 800, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, false, newMatchUpdate(&coinIDs[1], nil, &coinIDs[3]), newMatchUpdate(&coinIDs[4], nil, nil)),
					balances: map[uint32]*botBalance{
						42: {1e8 + 2e6 - 1000, 0, 0},
						0:  {1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 5e6) + 2800), 0, calc.BaseToQuote(6e7, 3e6) - 1200},
					},
					numPendingTrades: 1,
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[3], asset.Refund, b2q(6e7, 3e6), 1200, true),
						newWalletTx(coinIDs[4], asset.Swap, b2q(6e7, 2e6), 800, true),
						newWalletTx(coinIDs[5], asset.Redeem, 2e6, 700, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, true, newMatchUpdate(&coinIDs[1], nil, &coinIDs[3]), newMatchUpdate(&coinIDs[4], &coinIDs[5], nil)),
					balances: map[uint32]*botBalance{
						42: {1e8 + 4e6 - 1700, 0, 0},
						0:  {1e8 - (b2q(5e7, 2e6) + b2q(6e7, 2e6) + 2800 + 1200), 0, 0},
					},
				},
			},
		},
		{
			name: "dynamic swapper, token, sell",
			initialBalances: map[uint32]uint64{
				966001: 1e8,
				966:    1e8,
				60:     1e8,
			},
			isDynamicSwapper: map[uint32]bool{
				966001: true,
				966:    true,
				60:     true,
			},
			multiTrade: &core.MultiTradeForm{
				Host:  host,
				Sell:  true,
				Base:  60,
				Quote: 966001,
				Placements: []*core.QtyRate{
					{Qty: 5e6, Rate: 5e7},
					{Qty: 5e6, Rate: 6e7},
				},
			},
			initialLockedFunds: []*orderLockedFunds{
				newOrderLockedFunds(orderIDs[0], 5e6+2000, 0, 4000, 3000),
				newOrderLockedFunds(orderIDs[1], 5e6+2000, 0, 4000, 3000),
			},
			postTradeBalances: map[uint32]*botBalance{
				966001: {1e8, 0, 0},
				966:    {1e8 - 8000, 8000, 0},
				60:     {1e8 - (5e6+2000+3000)*2, (5e6 + 2000 + 3000) * 2, 0},
			},
			updatesAndBalances: []*updatesAndBalances{
				// First order has a match and sends a swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2e6, 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3e6+1000, 0, 4000, 3000, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[0], nil, nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8, 0, 0},
						966:    {1e8 - 8000, 8000, 0},
						60:     {1e8 - (5e6+2000+3000)*2, 3e6 + 1000 + 5e6 + 2000 + 3000 + 3000, 0},
					},
					numPendingTrades: 2,
				},
				// Second order has a match and sends swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3e6, 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 2e6+1000, 0, 4000, 3000, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[1], nil, nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8, 0, 0},
						966:    {1e8 - 8000, 8000, 0},
						60:     {1e8 - (5e6+2000+3000)*2, 3e6 + 1000 + 2e6 + 1000 + 3000 + 3000, 0},
					},
					numPendingTrades: 2,
				},
				// First order swap is confirmed, and redemption is sent
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, 2e6, 900, true),
						newWalletTx(coinIDs[2], asset.Redeem, b2q(5e7, 2e6), 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3e6+1000, 0, 3000, 3000, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[0], &coinIDs[2], nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8, 0, b2q(5e7, 2e6)},
						966:    {1e8 - 8000, 7000, 0},
						60:     {1e8 - (5e6+2000+3000)*2 + 100, 3e6 + 1000 + 2e6 + 1000 + 3000 + 3000, 0},
					},
					numPendingTrades: 2,
				},
				// First order redemption confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[2], asset.Redeem, b2q(5e7, 2e6), 800, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], 3e6+1000, 0, 3000, 3000, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[0], &coinIDs[2], nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8 + b2q(5e7, 2e6), 0, 0},
						966:    {1e8 - 7000 - 800, 7000, 0},
						60:     {1e8 - (5e6+2000+3000)*2 + 100, 3e6 + 1000 + 2e6 + 1000 + 3000 + 3000, 0},
					},
					numPendingTrades: 2,
				},
				// First order cancelled
				{
					orderUpdate: newOrderUpdate(orderIDs[0], 0, 0, 0, 0, order.OrderStatusCanceled, true, newMatchUpdate(&coinIDs[0], &coinIDs[2], nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8 + b2q(5e7, 2e6), 0, 0},
						966:    {1e8 - 4000 - 800, 4000, 0},
						60:     {1e8 - (7e6 + 900 + 2000 + 3000), 2e6 + 1000 + 3000, 0},
					},
					numPendingTrades: 1,
				},
				// Second order second match, swap sent, and first match refunded
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, 3e6, 1000, true),
						newWalletTx(coinIDs[3], asset.Refund, 3e6, 1200, false),
						newWalletTx(coinIDs[4], asset.Swap, 2e6, 800, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 4000, 1800, order.OrderStatusExecuted, false, newMatchUpdate(&coinIDs[1], nil, &coinIDs[3]), newMatchUpdate(&coinIDs[4], nil, nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8 + b2q(5e7, 2e6), 0, 0},
						966:    {1e8 - 4000 - 800, 4000, 0},
						60:     {1e8 - (7e6 + 900 + 2000 + 3000) + 200, 1800, 3e6},
					},
					numPendingTrades: 1,
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[3], asset.Refund, 3e6, 1100, true),
						newWalletTx(coinIDs[4], asset.Swap, 2e6, 800, true),
						newWalletTx(coinIDs[5], asset.Redeem, calc.BaseToQuote(6e7, 2e6), 700, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, true, newMatchUpdate(&coinIDs[1], nil, &coinIDs[3]), newMatchUpdate(&coinIDs[4], &coinIDs[5], nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8 + calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 2e6), 0, 0},
						966:    {1e8 - 1500, 0, 0},
						60:     {1e8 - (4e6 + 3800), 0, 0},
					},
				},
			},
		},
		{
			name: "dynamic swapper, token, buy",
			initialBalances: map[uint32]uint64{
				966001: 1e8,
				966:    1e8,
				60:     1e8,
			},
			isDynamicSwapper: map[uint32]bool{
				966001: true,
				966:    true,
				60:     true,
			},
			multiTrade: &core.MultiTradeForm{
				Host:  host,
				Sell:  false,
				Base:  60,
				Quote: 966001,
				Placements: []*core.QtyRate{
					{Qty: 5e6, Rate: 5e7},
					{Qty: 5e6, Rate: 6e7},
				},
			},
			initialLockedFunds: []*orderLockedFunds{
				newOrderLockedFunds(orderIDs[0], b2q(5e7, 5e6), 2000, 3000, 4000),
				newOrderLockedFunds(orderIDs[1], b2q(6e7, 5e6), 2000, 3000, 4000),
			},
			postTradeBalances: map[uint32]*botBalance{
				966001: {1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)), calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6), 0},
				966:    {1e8 - 12000, 12000, 0},
				60:     {1e8 - 6000, 6000, 0},
			},
			updatesAndBalances: []*updatesAndBalances{
				// First order has a match and sends a swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, calc.BaseToQuote(5e7, 2e6), 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], calc.BaseToQuote(5e7, 3e6), 1000, 3000, 4000, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[0], nil, nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)), calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 5e6), 0},
						966:    {1e8 - 12000, 11000, 0},
						60:     {1e8 - 6000, 6000, 0},
					},
					numPendingTrades: 2,
				},
				// Second order has a match and sends swap tx
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, calc.BaseToQuote(6e7, 3e6), 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], calc.BaseToQuote(6e7, 2e6), 1000, 3000, 4000, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[1], nil, nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)), calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 2e6), 0},
						966:    {1e8 - 12000, 10000, 0},
						60:     {1e8 - 6000, 6000, 0},
					},
					numPendingTrades: 2,
				},
				// First order swap is confirmed, and redemption is sent
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[0], asset.Swap, calc.BaseToQuote(5e7, 2e6), 900, true),
						newWalletTx(coinIDs[2], asset.Redeem, 2e6, 1000, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], calc.BaseToQuote(5e7, 3e6), 1000, 2000, 4000, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[0], &coinIDs[2], nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)), calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 2e6), 0},
						966:    {1e8 - 12000 + 100, 10000, 0},
						60:     {1e8 - 6000, 5000, 2e6},
					},
					numPendingTrades: 2,
				},
				// First order redemption confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[2], asset.Redeem, 2e6, 800, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[0], calc.BaseToQuote(5e7, 3e6), 1000, 2000, 4000, order.OrderStatusBooked, false, newMatchUpdate(&coinIDs[0], &coinIDs[2], nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8 - (calc.BaseToQuote(5e7, 5e6) + calc.BaseToQuote(6e7, 5e6)), calc.BaseToQuote(5e7, 3e6) + calc.BaseToQuote(6e7, 2e6), 0},
						966:    {1e8 - 12000 + 100, 10000, 0},
						60:     {1e8 - 5800 + 2e6, 5000, 0},
					},
					numPendingTrades: 2,
				},
				// First order cancelled
				{
					orderUpdate: newOrderUpdate(orderIDs[0], 0, 0, 0, 0, order.OrderStatusCanceled, true, newMatchUpdate(&coinIDs[0], &coinIDs[2], nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 5e6)), calc.BaseToQuote(6e7, 2e6), 0},
						966:    {1e8 - 7000 + 100, 5000, 0},
						60:     {1e8 + 2e6 - 3800, 3000, 0},
					},
					numPendingTrades: 1,
				},
				// Second order second match, swap sent, and first match refunded
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[1], asset.Swap, calc.BaseToQuote(6e7, 3e6), 1000, true),
						newWalletTx(coinIDs[3], asset.Refund, calc.BaseToQuote(6e7, 3e6), 1200, false),
						newWalletTx(coinIDs[4], asset.Swap, calc.BaseToQuote(6e7, 2e6), 800, false),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 3000, 2000, order.OrderStatusExecuted, false, newMatchUpdate(&coinIDs[1], nil, &coinIDs[3]), newMatchUpdate(&coinIDs[4], nil, nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 5e6)), 0, calc.BaseToQuote(6e7, 3e6)},
						966:    {1e8 - 6000 + 100, 2000, 0},
						60:     {1e8 + 2e6 - 3800, 3000, 0},
					},
					numPendingTrades: 1,
				},
				// Second order second match redeemed and confirmed, first match refund confirmed
				{
					txUpdates: []*asset.WalletTransaction{
						newWalletTx(coinIDs[3], asset.Refund, calc.BaseToQuote(6e7, 3e6), 1200, true),
						newWalletTx(coinIDs[4], asset.Swap, calc.BaseToQuote(6e7, 2e6), 800, true),
						newWalletTx(coinIDs[5], asset.Redeem, 2e6, 700, true),
					},
					orderUpdate: newOrderUpdate(orderIDs[1], 0, 0, 0, 0, order.OrderStatusExecuted, true, newMatchUpdate(&coinIDs[1], nil, &coinIDs[3]), newMatchUpdate(&coinIDs[4], &coinIDs[5], nil)),
					balances: map[uint32]*botBalance{
						966001: {1e8 - (calc.BaseToQuote(5e7, 2e6) + calc.BaseToQuote(6e7, 2e6)), 0, 0},
						966:    {1e8 - 3900, 0, 0},
						60:     {1e8 + 4e6 - 1500, 0, 0},
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
		tCore.market = mkt
		tCore.isDynamicSwapper = test.isDynamicSwapper

		multiTradeResult := make([]*core.Order, 0, len(test.initialLockedFunds))
		for _, o := range test.initialLockedFunds {
			multiTradeResult = append(multiTradeResult, &core.Order{
				Host:                 test.multiTrade.Host,
				BaseID:               test.multiTrade.Base,
				QuoteID:              test.multiTrade.Quote,
				Sell:                 test.multiTrade.Sell,
				LockedAmt:            o.lockedAmt,
				ID:                   o.id[:],
				ParentAssetLockedAmt: o.parentAssetLockedAmt,
				RedeemLockedAmt:      o.redeemLockedAmt,
				RefundLockedAmt:      o.refundLockedAmt,
			})
		}
		tCore.multiTradeResult = multiTradeResult

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		botID := dexMarketID(test.multiTrade.Host, test.multiTrade.Base, test.multiTrade.Quote)
		adaptor := unifiedExchangeAdaptorForBot(botID, test.initialBalances, nil, tCore, nil, tLogger)
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
				bal, err := adaptor.DEXBalance(assetID)
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
			tCore.walletTxsMtx.Lock()
			for _, txUpdate := range update.txUpdates {
				tCore.walletTxs[txUpdate.ID] = txUpdate
			}
			tCore.walletTxsMtx.Unlock()

			o := &core.Order{
				Host:                 test.multiTrade.Host,
				BaseID:               test.multiTrade.Base,
				QuoteID:              test.multiTrade.Quote,
				Sell:                 test.multiTrade.Sell,
				LockedAmt:            update.orderUpdate.lockedAmt,
				ID:                   update.orderUpdate.id[:],
				ParentAssetLockedAmt: update.orderUpdate.parentAssetLockedAmt,
				RedeemLockedAmt:      update.orderUpdate.redeemLockedAmt,
				RefundLockedAmt:      update.orderUpdate.refundLockedAmt,
				Status:               update.orderUpdate.status,
				Matches:              make([]*core.Match, len(update.orderUpdate.matches)),
				AllFeesConfirmed:     update.orderUpdate.allFeesConfirmed,
			}

			for i, matchUpdate := range update.orderUpdate.matches {
				o.Matches[i] = &core.Match{}
				if matchUpdate.swapCoin != nil {
					o.Matches[i].Swap = &core.Coin{
						ID: *matchUpdate.swapCoin,
					}
				}
				if matchUpdate.redeemCoin != nil {
					o.Matches[i].Redeem = &core.Coin{
						ID: *matchUpdate.redeemCoin,
					}
				}
				if matchUpdate.refundCoin != nil {
					o.Matches[i].Refund = &core.Coin{
						ID: *matchUpdate.refundCoin,
					}
				}
			}

			note := core.OrderNote{
				Order: o,
			}
			tCore.noteFeed <- &note
			tCore.noteFeed <- &core.BondPostNote{} // dummy note
			checkBalances(update.balances, i+1)

			if len(adaptor.pendingDEXOrders) != update.numPendingTrades {
				t.Fatalf("%s: update #%d, expected %d pending trades, got %d", test.name, i+1, update.numPendingTrades, len(adaptor.pendingDEXOrders))
			}
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

	coinID := encode.RandomBytes(32)
	txID := hex.EncodeToString(coinID)

	tests := []test{
		{
			name:         "withdrawer, not dynamic swapper",
			assetID:      42,
			isWithdrawer: true,
			depositAmt:   1e6,
			sendCoin: &tCoin{
				coinID: coinID,
				value:  1e6 - 2000,
			},
			unconfirmedTx: &asset.WalletTransaction{
				ID:        txID,
				Amount:    1e6 - 2000,
				Fees:      2000,
				Confirmed: false,
			},
			confirmedTx: &asset.WalletTransaction{
				ID:        txID,
				Amount:    1e6 - 2000,
				Fees:      2000,
				Confirmed: true,
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
				coinID: coinID,
				value:  1e6,
			},
			unconfirmedTx: &asset.WalletTransaction{
				ID:        txID,
				Amount:    1e6,
				Fees:      2000,
				Confirmed: false,
			},
			confirmedTx: &asset.WalletTransaction{
				ID:        txID,
				Amount:    1e6,
				Fees:      2000,
				Confirmed: true,
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
				coinID: coinID,
				value:  1e6,
			},
			unconfirmedTx: &asset.WalletTransaction{
				ID:        txID,
				Amount:    1e6,
				Fees:      4000,
				Confirmed: false,
			},
			confirmedTx: &asset.WalletTransaction{
				ID:        txID,
				Amount:    1e6,
				Fees:      2000,
				Confirmed: true,
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
				coinID: coinID,
				value:  1e6,
			},
			unconfirmedTx: &asset.WalletTransaction{
				ID:        txID,
				Amount:    1e6,
				Fees:      4000,
				Confirmed: false,
			},
			confirmedTx: &asset.WalletTransaction{
				ID:        txID,
				Amount:    1e6,
				Fees:      2000,
				Confirmed: true,
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
		fmt.Println("running test ", test.name)
		tCore := newTCore()
		tCore.isWithdrawer[test.assetID] = test.isWithdrawer
		tCore.isDynamicSwapper[test.assetID] = test.isDynamicSwapper
		tCore.setAssetBalances(map[uint32]uint64{test.assetID: test.initialDEXBalance, 0: 2e6, 966: 2e6})
		tCore.walletTxsMtx.Lock()
		tCore.walletTxs[test.unconfirmedTx.ID] = test.unconfirmedTx
		tCore.walletTxsMtx.Unlock()
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

		preConfirmBal, err := adaptor.DEXBalance(test.assetID)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if *preConfirmBal != *test.preConfirmDEXBalance {
			t.Fatalf("%s: unexpected pre confirm dex balance. want %d, got %d", test.name, test.preConfirmDEXBalance, preConfirmBal.Available)
		}

		if test.assetID == 966001 {
			preConfirmParentBal, err := adaptor.DEXBalance(966)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", test.name, err)
			}
			if preConfirmParentBal.Available != 2e6-test.unconfirmedTx.Fees {
				t.Fatalf("%s: unexpected pre confirm dex balance. want %d, got %d", test.name, test.preConfirmDEXBalance, preConfirmBal.Available)
			}
		}

		tCore.walletTxsMtx.Lock()
		tCore.walletTxs[test.unconfirmedTx.ID] = test.confirmedTx
		tCore.walletTxsMtx.Unlock()

		tCEX.confirmDeposit <- test.receivedAmt
		<-tCEX.confirmDepositComplete

		if test.isDynamicSwapper {
			time.Sleep(time.Millisecond * 100) // let the tx confirmation routine call WalletTransaction
		}

		postConfirmBal, err := adaptor.DEXBalance(test.assetID)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}

		if *postConfirmBal != *test.postConfirmDEXBalance {
			t.Fatalf("%s: unexpected post confirm dex balance. want %d, got %d", test.name, test.postConfirmDEXBalance, postConfirmBal)
		}

		if test.assetID == 966001 {
			postConfirmParentBal, err := adaptor.DEXBalance(966)
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
	coinID := encode.RandomBytes(32)
	txID := hex.EncodeToString(coinID)

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
				ID:        txID,
				Amount:    1e6 - 2000,
				Fees:      2000,
				Confirmed: true,
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

		tCore.walletTxsMtx.Lock()
		tCore.walletTxs[test.tx.ID] = test.tx
		tCore.walletTxsMtx.Unlock()

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

		preConfirmBal, err := adaptor.DEXBalance(assetID)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", test.name, err)
		}
		if *preConfirmBal != *test.preConfirmDEXBalance {
			t.Fatalf("%s: unexpected pre confirm dex balance. want %+v, got %+v", test.name, test.preConfirmDEXBalance, preConfirmBal)
		}

		tCEX.confirmWithdrawal <- &withdrawArgs{
			assetID: assetID,
			amt:     test.withdrawAmt,
			txID:    test.tx.ID,
		}

		<-tCEX.confirmWithdrawalComplete

		postConfirmBal, err := adaptor.DEXBalance(assetID)
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
		BaseID:           baseID,
		QuoteID:          quoteID,
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

		botID := dexMarketID(botCfg.Host, botCfg.BaseID, botCfg.QuoteID)
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
				bal, err := adaptor.CEXBalance(assetID)
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
			update := updateAndBalance.update
			update.ID = tradeID
			update.BaseID = baseID
			update.QuoteID = quoteID
			update.Sell = test.sell
			tCEX.tradeUpdates <- updateAndBalance.update
			tCEX.tradeUpdates <- &libxc.Trade{} // dummy update
			checkBalances(updateAndBalance.balances, i+1)
		}
	}

	for _, test := range tests {
		runTest(test)
	}
}
