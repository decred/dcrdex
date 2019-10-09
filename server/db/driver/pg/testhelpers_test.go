package pg

import (
	"os"
	"time"

	"github.com/decred/dcrdex/server/account"
	"github.com/decred/dcrdex/server/order"
	"github.com/decred/slog"
)

const LotSize = uint64(10_000_000_000)

var (
	AssetDCR uint32
	AssetBTC uint32
)

func startLogger() {
	logger := slog.NewBackend(os.Stdout).Logger("ORDER_DB_TEST")
	logger.SetLevel(slog.LevelDebug)
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
		MarketOrder: order.MarketOrder{
			Prefix: order.Prefix{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  order.LimitOrderType,
				ClientTime: time.Unix(1566497653+timeOffset, 0),
				ServerTime: time.Unix(1566497656+timeOffset, 0),
			},
			UTXOs: []order.UTXO{
				newUtxo("45b82138ca90e665a1c8793aa901aa232dd82be41b8e630dd621f24e717fc13a", 2),
			},
			Sell:     sell,
			Quantity: quantityLots * LotSize,
			Address:  addr,
		},
		Rate:  rate,
		Force: force,
	}
}
