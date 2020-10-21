package pg

import (
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
	LotSize         = uint64(10_000_000_000)
	EpochDuration   = uint64(10_000)
	MarketBuyBuffer = 1.1
)

// The asset integer IDs should set in TestMain or other bring up function (e.g.
// openDB()) prior to using them.
var (
	AssetDCR uint32
	AssetBTC uint32
	AssetLTC uint32
)

func randomBytes(len int) []byte {
	bytes := make([]byte, len)
	rand.Read(bytes)
	return bytes
}

func randomAccountID() account.AccountID {
	pk := randomBytes(pki.PubKeySize) // size is not important since it is going to be hashed
	return account.NewID(pk)
}

func randomPreimage() (pi order.Preimage) {
	rand.Read(pi[:])
	return
}

func randomCommitment() (com order.Commitment) {
	rand.Read(com[:])
	return
}

func mktConfig() (markets []*dex.MarketInfo) {
	mktConfig, err := dex.NewMarketInfoFromSymbols("DCR", "BTC", LotSize, EpochDuration, MarketBuyBuffer)
	if err != nil {
		panic(fmt.Sprintf("you broke it: %v", err))
	}
	markets = append(markets, mktConfig)

	mktConfig, err = dex.NewMarketInfoFromSymbols("BTC", "LTC", LotSize, EpochDuration, MarketBuyBuffer)
	if err != nil {
		panic(fmt.Sprintf("you broke it: %v", err))
	}
	markets = append(markets, mktConfig)

	// specify more here...
	return
}

func newMatch(maker *order.LimitOrder, taker order.Order, quantity uint64, epochID order.EpochID) *order.Match {
	return &order.Match{
		Maker:        maker,
		Taker:        taker,
		Quantity:     quantity,
		Rate:         maker.Rate,
		FeeRateBase:  12,
		FeeRateQuote: 14,
		Status:       order.NewlyMatched,
		Sigs:         order.Signatures{},
		Epoch:        epochID,
	}
}

func newLimitOrderRevealed(sell bool, rate, quantityLots uint64, force order.TimeInForce, timeOffset int64) (*order.LimitOrder, order.Preimage) {
	lo := newLimitOrder(sell, rate, quantityLots, force, timeOffset)
	pi := randomPreimage()
	lo.Commit = pi.Commit()
	return lo, pi
}

func newLimitOrder(sell bool, rate, quantityLots uint64, force order.TimeInForce, timeOffset int64) *order.LimitOrder {
	return newLimitOrderWithAssets(sell, rate, quantityLots, force, timeOffset, AssetDCR, AssetBTC)
}

func newLimitOrderWithAssets(sell bool, rate, quantityLots uint64, force order.TimeInForce, timeOffset int64, base, quote uint32) *order.LimitOrder {
	addr := "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui"
	if sell {
		addr = "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm"
	}
	return &order.LimitOrder{
		P: order.Prefix{
			AccountID:  randomAccountID(),
			BaseAsset:  base,
			QuoteAsset: quote,
			OrderType:  order.LimitOrderType,
			ClientTime: time.Unix(1566497653+timeOffset, 0).UTC(),
			ServerTime: time.Unix(1566497656+timeOffset, 0).UTC(),
			Commit:     randomCommitment(),
		},
		T: order.Trade{
			Coins: []order.CoinID{
				randomBytes(36),
				randomBytes(36),
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
			AccountID:  randomAccountID(),
			BaseAsset:  AssetDCR,
			QuoteAsset: AssetBTC,
			OrderType:  order.MarketOrderType,
			ClientTime: time.Unix(1566497653+timeOffset, 0).UTC(),
			ServerTime: time.Unix(1566497656+timeOffset, 0).UTC(),
			Commit:     randomCommitment(),
		},
		T: order.Trade{
			Coins:    []order.CoinID{randomBytes(36)},
			Sell:     true,
			Quantity: quantityLots * LotSize,
			Address:  "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm",
		},
	}
}

func newMarketBuyOrder(quantityQuoteAsset uint64, timeOffset int64) *order.MarketOrder {
	return &order.MarketOrder{
		P: order.Prefix{
			AccountID:  randomAccountID(),
			BaseAsset:  AssetDCR,
			QuoteAsset: AssetBTC,
			OrderType:  order.MarketOrderType,
			ClientTime: time.Unix(1566497653+timeOffset, 0).UTC(),
			ServerTime: time.Unix(1566497656+timeOffset, 0).UTC(),
			Commit:     randomCommitment(),
		},
		T: order.Trade{
			Coins:    []order.CoinID{randomBytes(36)},
			Sell:     false,
			Quantity: quantityQuoteAsset,
			Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
		},
	}
}

func newCancelOrder(targetOrderID order.OrderID, base, quote uint32, timeOffset int64) *order.CancelOrder {
	return &order.CancelOrder{
		P: order.Prefix{
			AccountID:  randomAccountID(),
			BaseAsset:  base,
			QuoteAsset: quote,
			OrderType:  order.CancelOrderType,
			ClientTime: time.Unix(1566497653+timeOffset, 0).UTC(),
			ServerTime: time.Unix(1566497656+timeOffset, 0).UTC(),
			Commit:     randomCommitment(),
		},
		TargetOrderID: targetOrderID,
	}
}
