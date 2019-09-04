package matcher

import (
	"fmt"
	"math"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/decred/dcrdex/server/account"
	"github.com/decred/dcrdex/server/market/order"
	"github.com/decred/slog"
)

// An arbitrary account ID for test orders.
var acct0 = account.AccountID{
	0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b, 0xd1, 0xff, 0x73, 0x15,
	0x90, 0xbc, 0xbd, 0xda, 0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1, 0x56, 0x99,
	0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25, 0xd5, 0x40,
}

const (
	AssetDCR uint32 = iota
	AssetBTC

	LotSize = uint64(10 * 1e8)
)

func startLogger() {
	logger := slog.NewBackend(os.Stdout).Logger("MATCHTEST")
	logger.SetLevel(slog.LevelDebug)
	UseLogger(logger)
}

var (
	marketOrders = []*order.MarketOrder{
		{ // market BUY of 4 lots
			Prefix: order.Prefix{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  order.MarketOrderType,
				ClientTime: time.Unix(1566497653, 0),
				ServerTime: time.Unix(1566497656, 0),
			},
			UTXOs:    []order.UTXO{},
			Sell:     false,
			Quantity: 4 * LotSize,
			Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
		},
		{ // market SELL of 2 lots
			Prefix: order.Prefix{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  order.MarketOrderType,
				ClientTime: time.Unix(1566497654, 0),
				ServerTime: time.Unix(1566497656, 0),
			},
			UTXOs:    []order.UTXO{},
			Sell:     true,
			Quantity: 2 * LotSize,
			Address:  "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm",
		},
	}

	limitOrders = []*order.LimitOrder{
		{ // limit BUY of 2 lots at 0.043
			MarketOrder: order.MarketOrder{
				Prefix: order.Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  order.LimitOrderType,
					ClientTime: time.Unix(1566497653, 0),
					ServerTime: time.Unix(1566497656, 0),
				},
				UTXOs:    []order.UTXO{},
				Sell:     false,
				Quantity: 2 * LotSize,
				Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
			},
			Rate:  0.043,
			Force: order.StandingTiF,
		},
		{ // limit SELL of 3 lots at 0.045
			MarketOrder: order.MarketOrder{
				Prefix: order.Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  order.LimitOrderType,
					ClientTime: time.Unix(1566497651, 0),
					ServerTime: time.Unix(1566497652, 0),
				},
				UTXOs:    []order.UTXO{},
				Sell:     true,
				Quantity: 3 * LotSize,
				Address:  "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm",
			},
			Rate:  0.045,
			Force: order.StandingTiF,
		},
		{ // limit BUY of 1 lot at 0.046
			MarketOrder: order.MarketOrder{
				Prefix: order.Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  order.LimitOrderType,
					ClientTime: time.Unix(1566497655, 0),
					ServerTime: time.Unix(1566497656, 0),
				},
				UTXOs:    []order.UTXO{},
				Sell:     false,
				Quantity: 1 * LotSize,
				Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
			},
			Rate:  0.046,
			Force: order.StandingTiF,
		},
		{ // limit BUY of 1 lot at 0.045
			MarketOrder: order.MarketOrder{
				Prefix: order.Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  order.LimitOrderType,
					ClientTime: time.Unix(1566497649, 0),
					ServerTime: time.Unix(1566497651, 0),
				},
				UTXOs:    []order.UTXO{},
				Sell:     false,
				Quantity: 1 * LotSize,
				Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
			},
			Rate:  0.045,
			Force: order.StandingTiF,
		},
	}
)

type BookStub struct {
	lotSize    uint64
	sellOrders []*order.LimitOrder // sorted descending in this stub
	buyOrders  []*order.LimitOrder // sorted ascending
}

func (b *BookStub) LotSize() uint64 {
	return b.lotSize
}

func (b *BookStub) BestSell() *order.LimitOrder {
	if len(b.sellOrders) == 0 {
		return nil
	}
	return b.sellOrders[len(b.sellOrders)-1]
}

func (b *BookStub) BestBuy() *order.LimitOrder {
	if len(b.buyOrders) == 0 {
		return nil
	}
	return b.buyOrders[len(b.buyOrders)-1]
}

func (b *BookStub) SellCount() int {
	return len(b.sellOrders)
}

func (b *BookStub) BuyCount() int {
	return len(b.buyOrders)
}

func (b *BookStub) Insert(ord *order.LimitOrder) {
	// Only "inserts" by making it the best order.
	if ord.Sell {
		b.sellOrders = append(b.sellOrders, ord)
	} else {
		b.buyOrders = append(b.buyOrders, ord)
	}
}

func (b *BookStub) Remove(orderID order.OrderID) (*order.LimitOrder, bool) {
	for i := range b.buyOrders {
		if b.buyOrders[i].ID() == orderID {
			//fmt.Println("Removing", orderID)
			removed := b.buyOrders[i]
			b.buyOrders = append(b.buyOrders[:i], b.buyOrders[i+1:]...)
			return removed, true
		}
	}
	for i := range b.sellOrders {
		if b.sellOrders[i].ID() == orderID {
			//fmt.Println("Removing", orderID)
			removed := b.sellOrders[i]
			b.sellOrders = append(b.sellOrders[:i], b.sellOrders[i+1:]...)
			return removed, true
		}
	}
	return nil, false
}

var _ Booker = (*BookStub)(nil)

func newLimitOrder(sell bool, rate float64, quantityLots uint64, force order.TimeInForce, timeOffset int64) *order.LimitOrder {
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
			UTXOs:    []order.UTXO{},
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
			AccountID:  acct0,
			BaseAsset:  AssetDCR,
			QuoteAsset: AssetBTC,
			OrderType:  order.MarketOrderType,
			ClientTime: time.Unix(1566497653+timeOffset, 0),
			ServerTime: time.Unix(1566497656+timeOffset, 0),
		},
		UTXOs:    []order.UTXO{},
		Sell:     true,
		Quantity: quantityLots * LotSize,
		Address:  "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm",
	}
}

func newMarketBuyOrder(quantityQuoteAsset uint64, timeOffset int64) *order.MarketOrder {
	return &order.MarketOrder{
		Prefix: order.Prefix{
			AccountID:  acct0,
			BaseAsset:  AssetDCR,
			QuoteAsset: AssetBTC,
			OrderType:  order.MarketOrderType,
			ClientTime: time.Unix(1566497653+timeOffset, 0),
			ServerTime: time.Unix(1566497656+timeOffset, 0),
		},
		UTXOs:    []order.UTXO{},
		Sell:     false,
		Quantity: quantityQuoteAsset,
		Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
	}
}

var (
	// Create a coherent order book of standing orders and sorted rates.
	bookBuyOrders = []*order.LimitOrder{
		newLimitOrder(false, 0.025, 2, order.StandingTiF, 0),
		newLimitOrder(false, 0.027, 2, order.StandingTiF, 0),
		newLimitOrder(false, 0.032, 2, order.StandingTiF, 0),
		newLimitOrder(false, 0.033, 2, order.StandingTiF, 0),
		newLimitOrder(false, 0.033, 1, order.StandingTiF, 2), // newer
		newLimitOrder(false, 0.036, 4, order.StandingTiF, 0),
		newLimitOrder(false, 0.039, 2, order.StandingTiF, 0),
		newLimitOrder(false, 0.040, 10, order.StandingTiF, 0),
		newLimitOrder(false, 0.043, 2, order.StandingTiF, 0),
		newLimitOrder(false, 0.043, 4, order.StandingTiF, 1), // newer
		newLimitOrder(false, 0.045, 1, order.StandingTiF, 0),
	}
	bookSellOrders = []*order.LimitOrder{
		newLimitOrder(true, 0.062, 2, order.StandingTiF, 0),
		newLimitOrder(true, 0.062, 2, order.StandingTiF, 1), // newer
		newLimitOrder(true, 0.061, 2, order.StandingTiF, 0),
		newLimitOrder(true, 0.060, 2, order.StandingTiF, 0),
		newLimitOrder(true, 0.055, 1, order.StandingTiF, 0),
		newLimitOrder(true, 0.054, 4, order.StandingTiF, 0),
		newLimitOrder(true, 0.050, 2, order.StandingTiF, 0),
		newLimitOrder(true, 0.047, 10, order.StandingTiF, 0),
		newLimitOrder(true, 0.047, 4, order.StandingTiF, 1), // newer
		newLimitOrder(true, 0.046, 2, order.StandingTiF, 0),
		newLimitOrder(true, 0.0455, 1, order.StandingTiF, 0),
	}
)

func newBooker() Booker {
	resetMakers()
	buyOrders := make([]*order.LimitOrder, len(bookBuyOrders))
	copy(buyOrders, bookBuyOrders)
	sellOrders := make([]*order.LimitOrder, len(bookSellOrders))
	copy(sellOrders, bookSellOrders)
	return &BookStub{
		lotSize:    LotSize,
		buyOrders:  buyOrders,
		sellOrders: sellOrders,
	}
}

func resetMakers() {
	for _, o := range bookBuyOrders {
		o.Filled = 0
	}
	for _, o := range bookSellOrders {
		o.Filled = 0
	}
}

func newMatch(taker order.Order, makers []*order.LimitOrder, lastPartialAmount ...uint64) *order.Match {
	amounts := make([]uint64, len(makers))
	rates := make([]float64, len(makers))
	var total uint64
	for i := range makers {
		total += makers[i].Quantity
		amounts[i] = makers[i].Quantity
		rates[i] = makers[i].Rate
	}
	if len(lastPartialAmount) > 0 {
		amounts[len(makers)-1] = lastPartialAmount[0]
		total -= makers[len(makers)-1].Quantity - lastPartialAmount[0]
	}
	return &order.Match{
		Taker:   taker,
		Makers:  makers,
		Amounts: amounts,
		Rates:   rates,
		Total:   total,
	}
}

func Test_matchLimitOrder(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	takers := []*order.LimitOrder{
		newLimitOrder(false, 0.0455, 1, order.ImmediateTiF, 0), // buy, 1 lot, immediate, equal rate
		newLimitOrder(true, 0.0445, 1, order.ImmediateTiF, 0),  // sell, 1 lot, immediate, overlapping rate
		newLimitOrder(true, 0.043, 5, order.StandingTiF, 0),    // sell, 5 lots, immediate, multiple makers
		newLimitOrder(true, 0.043, 4, order.StandingTiF, 0),    // sell, 4 lots, immediate, multiple makers, partial last maker
		newLimitOrder(true, 0.043, 8, order.StandingTiF, 0),    // sell, 8 lots, immediate, multiple makers, partial taker remaining
	}
	resetTakers := func() {
		for _, o := range takers {
			o.Filled = 0
		}
	}

	nSell := len(bookSellOrders)
	nBuy := len(bookBuyOrders)

	type args struct {
		book Booker
		ord  *order.LimitOrder
	}
	tests := []struct {
		name           string
		args           args
		doesMatch      bool
		wantMatch      *order.Match
		takerRemaining uint64
	}{
		{
			"OK limit buy immediate rate match",
			args{
				book: newBooker(),
				ord:  takers[0],
			},
			true,
			newMatch(takers[0], []*order.LimitOrder{bookSellOrders[nSell-1]}),
			0,
		},
		{
			"OK limit sell immediate rate overlap",
			args{
				book: newBooker(),
				ord:  takers[1],
			},
			true,
			newMatch(takers[1], []*order.LimitOrder{bookBuyOrders[nBuy-1]}),
			0,
		},
		{
			"OK limit sell immediate multiple makers",
			args{
				book: newBooker(),
				ord:  takers[2],
			},
			true,
			newMatch(takers[2], []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2]}),
			0,
		},
		{
			"OK limit sell immediate multiple makers partial maker fill",
			args{
				book: newBooker(),
				ord:  takers[3],
			},
			true,
			newMatch(takers[3], []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2]}, 3*LotSize),
			0,
		},
		{
			"OK limit sell immediate multiple makers partial taker fill",
			args{
				book: newBooker(),
				ord:  takers[4],
			},
			true,
			newMatch(takers[4], []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2], bookBuyOrders[nBuy-3]}),
			1 * LotSize,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetTakers()
			resetMakers()

			gotMatch := matchLimitOrder(tt.args.book, tt.args.ord)
			matchMade := gotMatch != nil
			if tt.doesMatch != matchMade {
				t.Errorf("Match expected = %v, got = %v", tt.doesMatch, matchMade)
			}
			if !reflect.DeepEqual(gotMatch, tt.wantMatch) {
				t.Errorf("matchLimitOrder() = %v, want %v", gotMatch, tt.wantMatch)
			}
			if tt.takerRemaining != tt.args.ord.Remaining() {
				t.Errorf("Taker remaining incorrect. Expected %d, got %d.",
					tt.takerRemaining, tt.args.ord.Remaining())
			}
		})
	}
}

func newCancelOrder(targetOrderID order.OrderID) *order.CancelOrder {
	return &order.CancelOrder{
		TargetOrderID: targetOrderID,
	}
}

func TestMatch_cancelOnly(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := New()

	fakeOrder := newLimitOrder(false, 0.0455, 1, order.ImmediateTiF, 0)

	// takers is heterogenous w.r.t. type
	takers := []order.Order{
		newCancelOrder(bookBuyOrders[3].ID()),
		newCancelOrder(fakeOrder.ID()),
	}

	//nSell := len(bookSellOrders)
	//nBuy := len(bookBuyOrders)

	type args struct {
		book  Booker
		queue []order.Order
	}
	tests := []struct {
		name            string
		args            args
		doesMatch       bool
		wantMatches     []*order.Match
		wantNumPassed   int
		wantNumFailed   int
		wantNumPartial  int
		wantNumInserted int
	}{
		{
			name: "cancel standing ok",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[0]},
			},
			doesMatch: true,
			wantMatches: []*order.Match{
				{Taker: takers[0], Makers: []*order.LimitOrder{bookBuyOrders[3]}},
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumInserted: 0,
		},
		{
			name: "cancel non-existent standing",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[1]},
			},
			doesMatch:       false,
			wantMatches:     nil,
			wantNumPassed:   0,
			wantNumFailed:   1,
			wantNumPartial:  0,
			wantNumInserted: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetMakers()

			// var cancels int
			// for _, oi := range tt.args.queue {
			// 	if oi.Type() == order.CancelOrderType {
			// 		cancels++
			// 	}
			// }

			numBuys0 := tt.args.book.BuyCount()

			matches, passed, failed, partial, inserted := me.Match(tt.args.book, tt.args.queue)
			matchMade := len(matches) > 0 && matches[0] != nil
			if tt.doesMatch != matchMade {
				t.Errorf("Match expected = %v, got = %v", tt.doesMatch, matchMade)
			}
			if len(matches) != len(tt.wantMatches) {
				t.Errorf("number of matches %d, expected %d", len(matches), len(tt.wantMatches))
			}
			for i := range matches {
				if !reflect.DeepEqual(matches[i], tt.wantMatches[i]) {
					t.Errorf("matches[%d] = %v, want %v", i, matches[i], tt.wantMatches[i])
				}
			}
			if len(passed) != tt.wantNumPassed {
				t.Errorf("number passed %d, expected %d", len(passed), tt.wantNumPassed)
			}
			if len(failed) != tt.wantNumFailed {
				t.Errorf("number failed %d, expected %d", len(failed), tt.wantNumFailed)
			}
			if len(partial) != tt.wantNumPartial {
				t.Errorf("number partial %d, expected %d", len(partial), tt.wantNumPartial)
			}
			if len(inserted) != tt.wantNumInserted {
				t.Errorf("number inserted %d, expected %d", len(inserted), tt.wantNumInserted)
			}

			numBuys1 := tt.args.book.BuyCount()
			if numBuys0-len(passed) != numBuys1 {
				t.Errorf("Buy side order book size %d, expected %d", numBuys1, numBuys0-len(passed))
			}
		})
	}
}

func TestMatch_limitsOnly(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := New()

	badLotsizeOrder := newLimitOrder(false, 0.05, 1, order.ImmediateTiF, 0)
	badLotsizeOrder.Quantity /= 2

	// takers is heterogenous w.r.t. type
	takers := []order.Order{
		newLimitOrder(false, 0.0455, 1, order.ImmediateTiF, 0), // buy, 1 lot, immediate, equal rate
		newLimitOrder(false, 0.0455, 2, order.StandingTiF, 0),  // buy, 2 lot, standing, equal rate, partial taker insert to book
		newLimitOrder(false, 0.0455, 2, order.ImmediateTiF, 0), // buy, 2 lot, immediate, equal rate, partial taker unfilled
		newLimitOrder(false, 0.041, 1, order.ImmediateTiF, 0),  // buy, 1 lot, immediate, unfilled fail
		newLimitOrder(true, 0.0454, 1, order.ImmediateTiF, 0),  // sell, 1 lot, immediate
	}

	resetTakers := func() {
		for _, o := range takers {
			switch ot := o.(type) {
			case *order.MarketOrder:
				ot.Filled = 0
			case *order.LimitOrder:
				ot.Filled = 0
			}
		}
	}

	nSell := len(bookSellOrders)
	//nBuy := len(bookBuyOrders)

	type args struct {
		book  Booker
		queue []order.Order
	}
	tests := []struct {
		name            string
		args            args
		doesMatch       bool
		wantMatches     []*order.Match
		wantNumPassed   int
		wantNumFailed   int
		wantNumPartial  int
		wantNumInserted int
	}{
		{
			name: "limit buy immediate rate match",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[0]},
			},
			doesMatch: true,
			wantMatches: []*order.Match{
				newMatch(takers[0], []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumInserted: 0,
		},
		{
			name: "limit buy standing partial taker inserted to book",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[1]},
			},
			doesMatch: true,
			wantMatches: []*order.Match{
				newMatch(takers[1], []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  1,
			wantNumInserted: 1,
		},
		{
			name: "limit buy immediate partial taker unfilled",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[2]},
			},
			doesMatch: true,
			wantMatches: []*order.Match{
				newMatch(takers[2], []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  1,
			wantNumInserted: 0,
		},
		{
			name: "limit buy immediate unfilled fail",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[3]},
			},
			doesMatch:       false,
			wantMatches:     nil,
			wantNumPassed:   0,
			wantNumFailed:   1,
			wantNumPartial:  0,
			wantNumInserted: 0,
		},
		{
			name: "bad lot size order",
			args: args{
				book:  newBooker(),
				queue: []order.Order{badLotsizeOrder},
			},
			doesMatch:       false,
			wantMatches:     nil,
			wantNumPassed:   0,
			wantNumFailed:   1,
			wantNumPartial:  0,
			wantNumInserted: 0,
		},
		{
			name: "limit buy standing partial taker inserted to book, then filled by down-queue sell",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[1], takers[4]},
			},
			doesMatch: true,
			wantMatches: []*order.Match{
				newMatch(takers[1], []*order.LimitOrder{bookSellOrders[nSell-1]}),
				{ // the maker is reduced by matching first item in the queue
					Taker:   takers[4],
					Makers:  []*order.LimitOrder{takers[1].(*order.LimitOrder)},
					Amounts: []uint64{1 * LotSize}, // 2 - 1
					Rates:   []float64{0.0455},
					Total:   1 * LotSize,
				},
			},
			wantNumPassed:   2,
			wantNumFailed:   0,
			wantNumPartial:  1,
			wantNumInserted: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetTakers()
			resetMakers()

			matches, passed, failed, partial, inserted := me.Match(tt.args.book, tt.args.queue)
			matchMade := len(matches) > 0 && matches[0] != nil
			if tt.doesMatch != matchMade {
				t.Errorf("Match expected = %v, got = %v", tt.doesMatch, matchMade)
			}
			if len(matches) != len(tt.wantMatches) {
				t.Errorf("number of matches %d, expected %d", len(matches), len(tt.wantMatches))
			}
			for i := range matches {
				if !reflect.DeepEqual(matches[i], tt.wantMatches[i]) {
					t.Errorf("matches[%d] = %v, want %v", i, matches[i], tt.wantMatches[i])
				}
			}
			if len(passed) != tt.wantNumPassed {
				t.Errorf("number passed %d, expected %d", len(passed), tt.wantNumPassed)
			}
			if len(failed) != tt.wantNumFailed {
				t.Errorf("number failed %d, expected %d", len(failed), tt.wantNumFailed)
			}
			if len(partial) != tt.wantNumPartial {
				t.Errorf("number partial %d, expected %d", len(partial), tt.wantNumPartial)
			}
			if len(inserted) != tt.wantNumInserted {
				t.Errorf("number inserted %d, expected %d", len(inserted), tt.wantNumInserted)
			}
		})
	}
}

func TestMatch_marketSellsOnly(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := New()

	badLotsizeOrder := newMarketSellOrder(1, 0)
	badLotsizeOrder.Quantity /= 2

	// takers is heterogenous w.r.t. type
	takers := []order.Order{
		newMarketSellOrder(1, 0),  // sell, 1 lot
		newMarketSellOrder(5, 0),  // sell, 2 lot
		newMarketSellOrder(6, 0),  // sell, 3 lot, partial maker fill
		newMarketSellOrder(99, 0), // sell, 99 lot, partial taker fill
	}

	resetTakers := func() {
		for _, o := range takers {
			switch ot := o.(type) {
			case *order.MarketOrder:
				ot.Filled = 0
			case *order.LimitOrder:
				ot.Filled = 0
			}
		}
	}

	//nSell := len(bookSellOrders)
	nBuy := len(bookBuyOrders)

	type args struct {
		book  Booker
		queue []order.Order
	}
	tests := []struct {
		name            string
		args            args
		doesMatch       bool
		wantMatches     []*order.Match
		wantNumPassed   int
		wantNumFailed   int
		wantNumPartial  int
		wantNumInserted int
	}{
		{
			name: "market sell, 1 maker match",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[0]},
			},
			doesMatch: true,
			wantMatches: []*order.Match{
				newMatch(takers[0], []*order.LimitOrder{bookBuyOrders[nBuy-1]}),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumInserted: 0,
		},
		{
			name: "market sell, 2 maker match",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[1]},
			},
			doesMatch: true,
			wantMatches: []*order.Match{
				newMatch(takers[1], []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2]}),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumInserted: 0,
		},
		{
			name: "market sell, 2 maker match, partial taker fill",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[2]},
			},
			doesMatch: true,
			wantMatches: []*order.Match{
				newMatch(takers[2], []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2], bookBuyOrders[nBuy-3]}, 1*LotSize),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumInserted: 0,
		},
		{
			name: "market sell bad lot size",
			args: args{
				book:  newBooker(),
				queue: []order.Order{badLotsizeOrder},
			},
			doesMatch:       false,
			wantMatches:     nil,
			wantNumPassed:   0,
			wantNumFailed:   1,
			wantNumPartial:  0,
			wantNumInserted: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetTakers()
			resetMakers()

			fmt.Printf("%v\n", takers)

			matches, passed, failed, partial, inserted := me.Match(tt.args.book, tt.args.queue)
			matchMade := len(matches) > 0 && matches[0] != nil
			if tt.doesMatch != matchMade {
				t.Errorf("Match expected = %v, got = %v", tt.doesMatch, matchMade)
			}
			if len(matches) != len(tt.wantMatches) {
				t.Errorf("number of matches %d, expected %d", len(matches), len(tt.wantMatches))
			}
			for i := range matches {
				if !reflect.DeepEqual(matches[i], tt.wantMatches[i]) {
					t.Errorf("matches[%d] = %v, want %v", i, matches[i], tt.wantMatches[i])
				}
			}
			if len(passed) != tt.wantNumPassed {
				t.Errorf("number passed %d, expected %d", len(passed), tt.wantNumPassed)
			}
			if len(failed) != tt.wantNumFailed {
				t.Errorf("number failed %d, expected %d", len(failed), tt.wantNumFailed)
			}
			if len(partial) != tt.wantNumPartial {
				t.Errorf("number partial %d, expected %d", len(partial), tt.wantNumPartial)
			}
			if len(inserted) != tt.wantNumInserted {
				t.Errorf("number inserted %d, expected %d", len(inserted), tt.wantNumInserted)
			}
		})
	}
}

func TestMatch_marketBuysOnly(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := New()

	nSell := len(bookSellOrders)
	//nBuy := len(bookBuyOrders)

	// marketBuyRate computes the effective price rate if the specified number
	// of lots were to be purchased given the current sell order book.
	marketBuyRate := func(lots int) float64 {
		var weightedRate float64
		if lots > len(bookSellOrders) {
			lots = len(bookSellOrders)
		}
		lotsRemaining := uint64(lots)
		var i int
		for lotsRemaining > 0 {
			orderLots := bookSellOrders[nSell-1-i].Quantity / LotSize
			if orderLots > lotsRemaining {
				orderLots = lotsRemaining
			}
			lotsRemaining -= orderLots
			i++
			weightedRate += float64(orderLots) * bookSellOrders[nSell-1-i].Rate
		}
		return weightedRate / float64(lots)
	}

	// buyLotsAmt computes the base asset amount required to purchase the
	// specified number of lots, where buying just 1 lot requires a buffer.
	buyLotsAmt := func(lots int) uint64 {
		totalBufferedBase := uint64(lots) * LotSize
		if lots < 2 {
			totalBufferedBase += LotSize / 2
		}
		return totalBufferedBase
	}

	// quoteAmt computes the required amount of the quote asset required to
	// purchase the specified number of lots given the current order book and
	// required amount buffering in the single lot case.
	quoteAmt := func(lots int) float64 {
		amt := float64(buyLotsAmt(lots)) * marketBuyRate(lots)
		return math.Nextafter(amt, amt+1)
	}
	quoteAmtInt := func(lots int) uint64 {
		return uint64(quoteAmt(lots))
	}

	// fmt.Printf("%d\n", buyLotsAmt(99))                 // exact cost of N lots in base asset
	// fmt.Printf("%f\n", quoteAmt(99))                   // float cost of N lots in quote asset
	// fmt.Printf("%f\n", quoteAmt(99)/marketBuyRate(99)) // back to base asset -- NOTE PRECISION LOSS!!!

	// takers is heterogenous w.r.t. type
	takers := []order.Order{
		newMarketBuyOrder(quoteAmtInt(1), 0),  // buy, 1 lot
		newMarketBuyOrder(quoteAmtInt(2), 0),  // buy, 2 lot
		newMarketBuyOrder(quoteAmtInt(3), 0),  // buy, 3 lot
		newMarketBuyOrder(quoteAmtInt(99), 0), // buy, 99 lot
	}

	resetTakers := func() {
		for _, o := range takers {
			switch ot := o.(type) {
			case *order.MarketOrder:
				ot.Filled = 0
			case *order.LimitOrder:
				ot.Filled = 0
			}
		}
	}

	bookSellOrdersReverse := make([]*order.LimitOrder, len(bookSellOrders))
	for i := range bookSellOrders {
		bookSellOrdersReverse[len(bookSellOrders)-1-i] = bookSellOrders[i]
	}

	type args struct {
		book  Booker
		queue []order.Order
	}
	tests := []struct {
		name            string
		args            args
		doesMatch       bool
		wantMatches     []*order.Match
		wantNumPassed   int
		wantNumFailed   int
		wantNumPartial  int
		wantNumInserted int
	}{
		{
			name: "market buy, 1 maker match",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[0]},
			},
			doesMatch: true,
			wantMatches: []*order.Match{
				newMatch(takers[0], []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumInserted: 0,
		},
		{
			name: "market buy, 2 maker match",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[1]},
			},
			doesMatch: true,
			wantMatches: []*order.Match{
				newMatch(takers[1], []*order.LimitOrder{bookSellOrders[nSell-1], bookSellOrders[nSell-2]}, 1*LotSize),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumInserted: 0,
		},
		{
			name: "market buy, 3 maker match",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[2]},
			},
			doesMatch: true,
			wantMatches: []*order.Match{
				newMatch(takers[2], []*order.LimitOrder{bookSellOrders[nSell-1], bookSellOrders[nSell-2]}),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumInserted: 0,
		},
		{
			name: "market buy, 99 maker match",
			args: args{
				book:  newBooker(),
				queue: []order.Order{takers[3]},
			},
			doesMatch: true,
			wantMatches: []*order.Match{
				newMatch(takers[3], bookSellOrdersReverse),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumInserted: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetTakers()
			resetMakers()

			fmt.Printf("%v\n", takers)

			matches, passed, failed, partial, inserted := me.Match(tt.args.book, tt.args.queue)
			matchMade := len(matches) > 0 && matches[0] != nil
			if tt.doesMatch != matchMade {
				t.Errorf("Match expected = %v, got = %v", tt.doesMatch, matchMade)
			}
			if len(matches) != len(tt.wantMatches) {
				t.Errorf("number of matches %d, expected %d", len(matches), len(tt.wantMatches))
			}
			for i := range matches {
				if !reflect.DeepEqual(matches[i], tt.wantMatches[i]) {
					t.Errorf("matches[%d] = %v, want %v", i, matches[i], tt.wantMatches[i])
				}
			}
			if len(passed) != tt.wantNumPassed {
				t.Errorf("number passed %d, expected %d", len(passed), tt.wantNumPassed)
			}
			if len(failed) != tt.wantNumFailed {
				t.Errorf("number failed %d, expected %d", len(failed), tt.wantNumFailed)
			}
			if len(partial) != tt.wantNumPartial {
				t.Errorf("number partial %d, expected %d", len(partial), tt.wantNumPartial)
			}
			if len(inserted) != tt.wantNumInserted {
				t.Errorf("number inserted %d, expected %d", len(inserted), tt.wantNumInserted)
			}
		})
	}
}
func Test_shuffleQueue(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// order queues to be shuffled
	q3_1 := []order.Order{
		limitOrders[0],
		marketOrders[0],
		marketOrders[1],
	}

	// q3_2 has same orders as q1 in different order
	q3_2 := []order.Order{
		marketOrders[0],
		limitOrders[0],
		marketOrders[1],
	}

	// q3Shuffled is the expected result of sorting q3_1 and q3_2
	q3Shuffled := []order.Order{
		limitOrders[0],
		marketOrders[0],
		marketOrders[1],
	}

	// shuffleQueue should work with nil slice
	var qNil []order.Order

	// shuffleQueue should work with empty slice
	qEmpty := []order.Order{}

	// shuffleQueue should work with single element slice
	q1 := []order.Order{
		marketOrders[0],
	}

	// shuffleQueue should work with two element slice
	q2_a := []order.Order{
		limitOrders[0],
		marketOrders[0],
	}

	// ... with same output regardless of input order
	q2_b := []order.Order{
		marketOrders[0],
		limitOrders[0],
	}

	// repeated orders should be handled
	qDup := []order.Order{
		marketOrders[0],
		marketOrders[0],
	}

	q2Shuffled := []order.Order{
		limitOrders[0],
		marketOrders[0],
	}

	tests := []struct {
		name  string
		inOut []order.Order
		want  []order.Order
	}{
		{
			"q3_1 iter 1",
			q3_1,
			q3Shuffled,
		},
		{
			"q3_1 iter 2",
			q3_1,
			q3Shuffled,
		},
		{
			"q3_1 iter 3",
			q3_1,
			q3Shuffled,
		},
		{
			"q3_2",
			q3_2,
			q3Shuffled,
		},
		{
			"qEmpty",
			qEmpty,
			[]order.Order{},
		},
		{
			"qNil",
			qNil,
			[]order.Order(nil),
		},
		{
			"q1",
			q1,
			[]order.Order{q2Shuffled[1]},
		},
		{
			"q2_a",
			q2_a,
			q2Shuffled,
		},
		{
			"q2_b",
			q2_b,
			q2Shuffled,
		},
		{
			"qDup",
			qDup,
			[]order.Order{q2Shuffled[1], q2Shuffled[1]},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shuffleQueue(tt.inOut)
			if !reflect.DeepEqual(tt.inOut, tt.want) {
				t.Errorf("shuffleQueue(q): q = %#v, want %#v", tt.inOut, tt.want)
			}
		})
	}
}

func Test_sortQueue(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// order queues to be sorted
	q3_1 := []order.Order{
		limitOrders[0],
		marketOrders[0],
		marketOrders[1],
	}

	// q3_2 has same orders as q1 in different order
	q3_2 := []order.Order{
		marketOrders[0],
		limitOrders[0],
		marketOrders[1],
	}

	// q3Sorted is the expected result of sorting q3_1 and q3_2
	q3Sorted := []order.Order{
		limitOrders[0],
		marketOrders[0],
		marketOrders[1],
	}

	// sortQueue should work with nil slice
	var qNil []order.Order

	// sortQueue should work with empty slice
	qEmpty := []order.Order{}

	// sortQueue should work with single element slice
	q1 := []order.Order{
		marketOrders[0],
	}

	// sortQueue should work with two element slice
	q2_a := []order.Order{
		limitOrders[0],
		marketOrders[0],
	}

	// ... with same output regardless of input order
	q2_b := []order.Order{
		marketOrders[0],
		limitOrders[0],
	}

	// repeated orders should be handled
	qDup := []order.Order{
		marketOrders[0],
		marketOrders[0],
	}

	q2Sorted := []order.Order{
		limitOrders[0],
		marketOrders[0],
	}

	tests := []struct {
		name  string
		inOut []order.Order
		want  []order.Order
	}{
		{
			"q3_1 iter 1",
			q3_1,
			q3Sorted,
		},
		{
			"q3_1 iter 2",
			q3_1,
			q3Sorted,
		},
		{
			"q3_1 iter 3",
			q3_1,
			q3Sorted,
		},
		{
			"q3_2",
			q3_2,
			q3Sorted,
		},
		{
			"qEmpty",
			qEmpty,
			[]order.Order{},
		},
		{
			"qNil",
			qNil,
			[]order.Order(nil),
		},
		{
			"q1",
			q1,
			[]order.Order{q2Sorted[1]},
		},
		{
			"q2_a",
			q2_a,
			q2Sorted,
		},
		{
			"q2_b",
			q2_b,
			q2Sorted,
		},
		{
			"qDup",
			qDup,
			[]order.Order{q2Sorted[1], q2Sorted[1]},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortQueue(tt.inOut)
			if !reflect.DeepEqual(tt.inOut, tt.want) {
				t.Errorf("sortQueue(q): q = %#v, want %#v", tt.inOut, tt.want)
			}
		})
	}
}

func TestOrdersMatch(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	type args struct {
		a order.Order
		b order.Order
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			"MATCH market buy : limit sell",
			args{
				marketOrders[0],
				limitOrders[1],
			},
			true,
		},
		{
			"MATCH market sell : limit buy",
			args{
				marketOrders[1],
				limitOrders[0],
			},
			true,
		},
		{
			"MATCH limit sell : market buy",
			args{
				limitOrders[1],
				marketOrders[0],
			},
			true,
		},
		{
			"MATCH limit buy : market sell",
			args{
				limitOrders[0],
				marketOrders[1],
			},
			true,
		},
		{
			"NO MATCH market sell : market buy",
			args{
				marketOrders[1],
				marketOrders[0],
			},
			false,
		},
		{
			"NO MATCH (rates) limit sell : limit buy",
			args{
				limitOrders[0],
				limitOrders[1],
			},
			false,
		},
		{
			"NO MATCH (rates) limit buy : limit sell",
			args{
				limitOrders[1],
				limitOrders[0],
			},
			false,
		},
		{
			"MATCH (overlapping rates) limit sell : limit buy",
			args{
				limitOrders[1],
				limitOrders[2],
			},
			true,
		},
		{
			"MATCH (same rates) limit sell : limit buy",
			args{
				limitOrders[1],
				limitOrders[3],
			},
			true,
		},
		{
			"NO MATCH (same side) limit buy : limit buy",
			args{
				limitOrders[2],
				limitOrders[3],
			},
			false,
		},
		{
			"NO MATCH (cancel) market buy : cancel",
			args{
				marketOrders[0],
				&order.CancelOrder{},
			},
			false,
		},
		{
			"NO MATCH (cancel) cancel : market sell",
			args{
				&order.CancelOrder{},
				marketOrders[1],
			},
			false,
		},
		{
			"NO MATCH (cancel) limit sell : cancel",
			args{
				limitOrders[1],
				&order.CancelOrder{},
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OrdersMatch(tt.args.a, tt.args.b); got != tt.want {
				t.Errorf("OrdersMatch() = %v, want %v", got, tt.want)
			}
		})
	}
}
