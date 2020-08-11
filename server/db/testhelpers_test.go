package db

import (
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

const (
	LotSize         = uint64(10_000_000_000)
	EpochDuration   = uint64(10_000)
	MarketBuyBuffer = 1.1
)

// The asset integer IDs should set in TestMain or other bring up function (e.g.
// openDB()) prior to using them.
var (
	AssetDCR uint32
	AssetBTC uint32
)

func startLogger() {
	logger := dex.StdOutLogger("ORDER_DB_TEST", dex.LevelTrace)
	UseLogger(logger)
}

var acct0 = account.AccountID{
	0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73, 0x15,
	0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1, 0x56, 0x99,
	0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25, 0xd5, 0x40,
}

func newLimitOrder(sell bool, rate, quantityLots uint64, force order.TimeInForce, timeOffset int64) *order.LimitOrder {
	addr := "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui"
	if sell {
		addr = "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm"
	}
	return &order.LimitOrder{
		P: order.Prefix{
			AccountID:  acct0,
			BaseAsset:  AssetDCR,
			QuoteAsset: AssetBTC,
			OrderType:  order.LimitOrderType,
			ClientTime: time.Unix(1566497653+timeOffset, 0),
			ServerTime: time.Unix(1566497656+timeOffset, 0),
		},
		T: order.Trade{
			Coins: []order.CoinID{
				{
					0x45, 0xb8, 0x21, 0x38, 0xca, 0x90, 0xe6, 0x65, 0xa1, 0xc8, 0x79, 0x3a,
					0xa9, 0x01, 0xaa, 0x23, 0x2d, 0xd8, 0x2b, 0xe4, 0x1b, 0x8e, 0x63, 0x0d,
					0xd6, 0x21, 0xf2, 0x4e, 0x71, 0x7f, 0xc1, 0x3a, 0x00, 0x00, 0x00, 0x02,
				},
			},
			Sell:     sell,
			Quantity: quantityLots * LotSize,
			Address:  addr,
		},
		Rate:  rate,
		Force: force,
	}
}

func newMarketSellOrder(quantityLots uint64, timeOffset int64) *order.MarketOrder {
	return &order.MarketOrder{
		P: order.Prefix{
			AccountID:  acct0,
			BaseAsset:  AssetDCR,
			QuoteAsset: AssetBTC,
			OrderType:  order.MarketOrderType,
			ClientTime: time.Unix(1566497653+timeOffset, 0),
			ServerTime: time.Unix(1566497656+timeOffset, 0),
		},
		T: order.Trade{
			Coins:    []order.CoinID{},
			Sell:     true,
			Quantity: quantityLots * LotSize,
			Address:  "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm",
		},
	}
}

func newMarketBuyOrder(quantityQuoteAsset uint64, timeOffset int64) *order.MarketOrder {
	return &order.MarketOrder{
		P: order.Prefix{
			AccountID:  acct0,
			BaseAsset:  AssetDCR,
			QuoteAsset: AssetBTC,
			OrderType:  order.MarketOrderType,
			ClientTime: time.Unix(1566497653+timeOffset, 0),
			ServerTime: time.Unix(1566497656+timeOffset, 0),
		},
		T: order.Trade{
			Coins:    []order.CoinID{},
			Sell:     false,
			Quantity: quantityQuoteAsset,
			Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
		},
	}
}

func newCancelOrder(targetOrderID order.OrderID, base, quote uint32, timeOffset int64) *order.CancelOrder {
	return &order.CancelOrder{
		P: order.Prefix{
			AccountID:  acct0,
			BaseAsset:  base,
			QuoteAsset: quote,
			OrderType:  order.CancelOrderType,
			ClientTime: time.Unix(1566497653+timeOffset, 0),
			ServerTime: time.Unix(1566497656+timeOffset, 0),
		},
		TargetOrderID: targetOrderID,
	}
}
