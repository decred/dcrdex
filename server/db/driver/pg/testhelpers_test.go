package pg

import (
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/account/pki"
	"github.com/decred/slog"
)

func startLogger() {
	logger := slog.NewBackend(os.Stdout).Logger("PG_DB_TEST")
	logger.SetLevel(slog.LevelDebug)
	UseLogger(logger)
}

const (
	LotSize       = uint64(10_000_000_000)
	EpochDuration = uint64(10)
)

// The asset integer IDs should set in TestMain or other bring up function (e.g.
// openDB()) prior to using them.
var (
	AssetDCR uint32
	AssetBTC uint32
)

func randomBytes(len int) []byte {
	bytes := make([]byte, len)
	rand.Read(bytes)
	return bytes
}

func randomHashStr() string {
	return hex.EncodeToString(randomBytes(32))
}

func randomAccountID() account.AccountID {
	pk := randomBytes(pki.PubKeySize) // size is not important since it is going to be hashed
	return account.NewID(pk)
}

func mktConfig() (markets []*dex.MarketInfo) {
	mktConfig, err := dex.NewMarketInfoFromSymbols("DCR", "BTC", LotSize, EpochDuration)
	if err != nil {
		panic(fmt.Sprintf("you broke it: %v", err))
	}

	markets = append(markets, mktConfig)
	// specify more here...
	return
}

func newMatch(maker *order.LimitOrder, taker order.Order, quantity uint64, epochID order.EpochID) *order.Match {
	return &order.Match{
		Maker:    maker,
		Taker:    taker,
		Quantity: quantity,
		Rate:     maker.Rate,
		Status:   order.NewlyMatched,
		Sigs:     order.Signatures{},
		Epoch:    epochID,
	}
}

func newLimitOrder(sell bool, rate, quantityLots uint64, force order.TimeInForce, timeOffset int64) *order.LimitOrder {
	addr := "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui"
	if sell {
		addr = "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm"
	}
	return &order.LimitOrder{
		MarketOrder: order.MarketOrder{
			Prefix: order.Prefix{
				AccountID:  randomAccountID(),
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  order.LimitOrderType,
				ClientTime: time.Unix(1566497653+timeOffset, 0).UTC(),
				ServerTime: time.Unix(1566497656+timeOffset, 0).UTC(),
			},
			UTXOs: []order.Outpoint{
				newUtxo(randomHashStr(), 2),
				newUtxo(randomHashStr(), 0),
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
		Prefix: order.Prefix{
			AccountID:  randomAccountID(),
			BaseAsset:  AssetDCR,
			QuoteAsset: AssetBTC,
			OrderType:  order.MarketOrderType,
			ClientTime: time.Unix(1566497653+timeOffset, 0).UTC(),
			ServerTime: time.Unix(1566497656+timeOffset, 0).UTC(),
		},
		UTXOs:    []order.Outpoint{newUtxo(randomHashStr(), 4)},
		Sell:     true,
		Quantity: quantityLots * LotSize,
		Address:  "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm",
	}
}

func newMarketBuyOrder(quantityQuoteAsset uint64, timeOffset int64) *order.MarketOrder {
	return &order.MarketOrder{
		Prefix: order.Prefix{
			AccountID:  randomAccountID(),
			BaseAsset:  AssetDCR,
			QuoteAsset: AssetBTC,
			OrderType:  order.MarketOrderType,
			ClientTime: time.Unix(1566497653+timeOffset, 0).UTC(),
			ServerTime: time.Unix(1566497656+timeOffset, 0).UTC(),
		},
		UTXOs:    []order.Outpoint{newUtxo(randomHashStr(), 3)},
		Sell:     false,
		Quantity: quantityQuoteAsset,
		Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
	}
}

func newCancelOrder(targetOrderID order.OrderID, base, quote uint32, timeOffset int64) *order.CancelOrder {
	return &order.CancelOrder{
		Prefix: order.Prefix{
			AccountID:  randomAccountID(),
			BaseAsset:  base,
			QuoteAsset: quote,
			OrderType:  order.CancelOrderType,
			ClientTime: time.Unix(1566497653+timeOffset, 0).UTC(),
			ServerTime: time.Unix(1566497656+timeOffset, 0).UTC(),
		},
		TargetOrderID: targetOrderID,
	}
}
