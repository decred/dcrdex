// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"encoding/hex"
	"testing"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
)

func TestValidateOrder(t *testing.T) {
	mktInfo, err := dex.NewMarketInfoFromSymbols("dcr", "btc", LotSize, EpochDuration, MarketBuyBuffer)
	if err != nil {
		t.Fatalf("invalid market: %v", err)
	}

	AssetDCR = mktInfo.Base
	AssetBTC = mktInfo.Quote

	orderBadLotSize := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	orderBadLotSize.Quantity /= 2

	orderBadType := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	orderBadType.OrderType = order.CancelOrderType // bad type

	orderBadMarket := newLimitOrder(false, 4900000, 1, order.StandingTiF, 0)
	orderBadMarket.BaseAsset = AssetDCR
	orderBadMarket.QuoteAsset = AssetDCR // same as base

	marketOrderBadType := newMarketSellOrder(1, 0)
	marketOrderBadType.OrderType = order.CancelOrderType // wrong type

	marketOrderBadAmt := newMarketSellOrder(1, 0)
	marketOrderBadAmt.Quantity /= 2

	marketOrderBadRemaining := newMarketSellOrder(1, 0)
	marketOrderBadRemaining.FillAmt = marketOrderBadAmt.Quantity / 2

	// order ID for a cancel order
	orderID0, _ := hex.DecodeString("dd64e2ae2845d281ba55a6d46eceb9297b2bdec5c5bada78f9ae9e373164df0d")
	var targetOrderID order.OrderID
	copy(targetOrderID[:], orderID0)

	cancelOrderBadType := newCancelOrder(targetOrderID, AssetDCR, AssetBTC, 0)
	cancelOrderBadType.OrderType = order.LimitOrderType // wrong type

	type args struct {
		ord    order.Order
		status order.OrderStatus
		mkt    *dex.MarketInfo
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "limit booked immediate (bad)",
			args: args{
				ord:    newLimitOrder(false, 4900000, 1, order.ImmediateTiF, 0),
				status: order.OrderStatusBooked,
				mkt:    mktInfo,
			},
			want: false,
		},
		{
			name: "unknown order status",
			args: args{
				ord:    newMarketSellOrder(1, 0),
				status: order.OrderStatusUnknown,
				mkt:    mktInfo,
			},
			want: false,
		},
		{
			name: "bad lot size",
			args: args{
				ord:    orderBadLotSize,
				status: order.OrderStatusEpoch,
				mkt:    mktInfo,
			},
			want: false,
		},
		{
			name: "bad order type",
			args: args{
				ord:    orderBadType,
				status: order.OrderStatusEpoch,
				mkt:    mktInfo,
			},
			want: false,
		},
		{
			name: "bad market",
			args: args{
				ord:    orderBadMarket,
				status: order.OrderStatusEpoch,
				mkt:    mktInfo,
			},
			want: false,
		},
		{
			name: "ok",
			args: args{
				ord:    newMarketSellOrder(1, 0),
				status: order.OrderStatusEpoch,
				mkt:    mktInfo,
			},
			want: true,
		},
		{
			name: "market order bad status (booked)",
			args: args{
				ord:    newMarketSellOrder(1, 0),
				status: order.OrderStatusBooked,
				mkt:    mktInfo,
			},
			want: false,
		},
		{
			name: "market order bad status (canceled)",
			args: args{
				ord:    newMarketSellOrder(1, 0),
				status: order.OrderStatusCanceled,
				mkt:    mktInfo,
			},
			want: false,
		},
		{
			name: "market order bad type",
			args: args{
				ord:    marketOrderBadType,
				status: order.OrderStatusEpoch,
				mkt:    mktInfo,
			},
			want: false,
		},
		{
			name: "market order bad amount",
			args: args{
				ord:    marketOrderBadAmt,
				status: order.OrderStatusEpoch,
				mkt:    mktInfo,
			},
			want: false,
		},
		{
			name: "market order bad remaining",
			args: args{
				ord:    marketOrderBadRemaining,
				status: order.OrderStatusEpoch,
				mkt:    mktInfo,
			},
			want: false,
		},
		{
			name: "cancel bad status (booked)",
			args: args{
				ord:    newCancelOrder(targetOrderID, AssetDCR, AssetBTC, 0),
				status: order.OrderStatusBooked,
				mkt:    mktInfo,
			},
			want: false,
		},
		{
			name: "cancel bad status (canceled)",
			args: args{
				ord:    newCancelOrder(targetOrderID, AssetDCR, AssetBTC, 0),
				status: order.OrderStatusBooked,
				mkt:    mktInfo,
			},
			want: false,
		},
		{
			name: "cancel bad status (booked)",
			args: args{
				ord:    newCancelOrder(targetOrderID, AssetDCR, AssetBTC, 0),
				status: order.OrderStatusBooked,
				mkt:    mktInfo,
			},
			want: false,
		},
		{
			name: "cancel bad type (limit)",
			args: args{
				ord:    cancelOrderBadType,
				status: order.OrderStatusEpoch,
				mkt:    mktInfo,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidateOrder(tt.args.ord, tt.args.status, tt.args.mkt); got != tt.want {
				t.Errorf("ValidateOrder() = %v, want %v", got, tt.want)
			}
		})
	}
}
