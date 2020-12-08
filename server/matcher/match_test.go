package matcher

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
)

// An arbitrary account ID for test orders.
var acct0 = account.AccountID{
	0x22, 0x4c, 0xba, 0xaa, 0xfa, 0x80, 0xbf, 0x3b,
	0xd1, 0xff, 0x73, 0x15, 0x90, 0xbc, 0xbd, 0xda,
	0x5a, 0x76, 0xf9, 0x1e, 0x60, 0xa1, 0x56, 0x99,
	0x46, 0x34, 0xe9, 0x1c, 0xec, 0x25, 0xd5, 0x40, // 32 bytes
}

const (
	AssetDCR uint32 = iota
	AssetBTC

	LotSize = uint64(10 * 1e8)
)

func randomPreimage() (pe order.Preimage) {
	rand.Read(pe[:])
	return
}

func startLogger() {
	logger := dex.StdOutLogger("MATCHTEST", dex.LevelTrace)
	UseLogger(logger)
}

var (
	marketPreimages = []order.Preimage{
		randomPreimage(),
		randomPreimage(),
	}
	marketOrders = []*OrderRevealed{
		{ // market BUY of 4 lots
			&order.MarketOrder{
				P: order.Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  order.MarketOrderType,
					ClientTime: time.Unix(1566497653, 0),
					ServerTime: time.Unix(1566497656, 0),
					Commit:     marketPreimages[0].Commit(),
				},
				T: order.Trade{
					Coins:    []order.CoinID{},
					Sell:     false,
					Quantity: 4 * LotSize,
					Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
				},
			},
			marketPreimages[0],
		},
		{ // market SELL of 2 lots
			&order.MarketOrder{
				P: order.Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  order.MarketOrderType,
					ClientTime: time.Unix(1566497654, 0),
					ServerTime: time.Unix(1566497656, 0),
					Commit:     marketPreimages[1].Commit(),
				},
				T: order.Trade{
					Coins:    []order.CoinID{},
					Sell:     true,
					Quantity: 2 * LotSize,
					Address:  "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm",
				},
			},
			marketPreimages[1],
		},
	}

	limitPreimages = []order.Preimage{
		randomPreimage(),
		randomPreimage(),
		randomPreimage(),
		randomPreimage(),
	}
	limitOrders = []*OrderRevealed{
		{ // limit BUY of 2 lots at 0.043
			&order.LimitOrder{
				P: order.Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  order.LimitOrderType,
					ClientTime: time.Unix(1566497653, 0),
					ServerTime: time.Unix(1566497656, 0),
					Commit:     limitPreimages[0].Commit(),
				},
				T: order.Trade{
					Coins:    []order.CoinID{},
					Sell:     false,
					Quantity: 2 * LotSize,
					Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
				},
				Rate:  4300000,
				Force: order.StandingTiF,
			},
			limitPreimages[0],
		},
		{ // limit SELL of 3 lots at 0.045
			&order.LimitOrder{
				P: order.Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  order.LimitOrderType,
					ClientTime: time.Unix(1566497651, 0),
					ServerTime: time.Unix(1566497652, 0),
					Commit:     limitPreimages[1].Commit(),
				},
				T: order.Trade{
					Coins:    []order.CoinID{},
					Sell:     true,
					Quantity: 3 * LotSize,
					Address:  "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm",
				},
				Rate:  4500000,
				Force: order.StandingTiF,
			},
			limitPreimages[1],
		},
		{ // limit BUY of 1 lot at 0.046
			&order.LimitOrder{
				P: order.Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  order.LimitOrderType,
					ClientTime: time.Unix(1566497655, 0),
					ServerTime: time.Unix(1566497656, 0),
					Commit:     limitPreimages[2].Commit(),
				},
				T: order.Trade{
					Coins:    []order.CoinID{},
					Sell:     false,
					Quantity: 1 * LotSize,
					Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
				},
				Rate:  4600000,
				Force: order.StandingTiF,
			},
			limitPreimages[2],
		},
		{ // limit BUY of 1 lot at 0.045
			&order.LimitOrder{
				P: order.Prefix{
					AccountID:  acct0,
					BaseAsset:  AssetDCR,
					QuoteAsset: AssetBTC,
					OrderType:  order.LimitOrderType,
					ClientTime: time.Unix(1566497649, 0),
					ServerTime: time.Unix(1566497651, 0),
					Commit:     limitPreimages[3].Commit(),
				},
				T: order.Trade{
					Coins:    []order.CoinID{},
					Sell:     false,
					Quantity: 1 * LotSize,
					Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
				},
				Rate:  4500000,
				Force: order.StandingTiF,
			},
			limitPreimages[3],
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

func (b *BookStub) Insert(ord *order.LimitOrder) bool {
	// Only "inserts" by making it the best order.
	if ord.Sell {
		b.sellOrders = append(b.sellOrders, ord)
	} else {
		b.buyOrders = append(b.buyOrders, ord)
	}
	return true
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

func (b *BookStub) BuyOrders() []*order.LimitOrder  { return b.buyOrders }
func (b *BookStub) SellOrders() []*order.LimitOrder { return b.sellOrders }

var _ Booker = (*BookStub)(nil)

func newLimitOrder(sell bool, rate, quantityLots uint64, force order.TimeInForce, timeOffset int64) *order.LimitOrder {
	return newLimit(sell, rate, quantityLots, force, timeOffset).Order.(*order.LimitOrder)
}

func newLimit(sell bool, rate, quantityLots uint64, force order.TimeInForce, timeOffset int64) *OrderRevealed {
	addr := "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui"
	if sell {
		addr = "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm"
	}
	pe := randomPreimage()
	return &OrderRevealed{
		&order.LimitOrder{
			P: order.Prefix{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  order.LimitOrderType,
				ClientTime: time.Unix(1566497653+timeOffset, 0),
				ServerTime: time.Unix(1566497656+timeOffset, 0),
				Commit:     pe.Commit(),
			},
			T: order.Trade{
				Coins:    []order.CoinID{},
				Sell:     sell,
				Quantity: quantityLots * LotSize,
				Address:  addr,
			},
			Rate:  rate,
			Force: force,
		},
		pe,
	}
}

func newMarketSellOrder(quantityLots uint64, timeOffset int64) *OrderRevealed {
	pe := randomPreimage()
	return &OrderRevealed{
		&order.MarketOrder{
			P: order.Prefix{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  order.MarketOrderType,
				ClientTime: time.Unix(1566497653+timeOffset, 0),
				ServerTime: time.Unix(1566497656+timeOffset, 0),
				Commit:     pe.Commit(),
			},
			T: order.Trade{
				Coins:    []order.CoinID{},
				Sell:     true,
				Quantity: quantityLots * LotSize,
				Address:  "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm",
			},
		},
		pe,
	}
}

func newMarketBuyOrder(quantityQuoteAsset uint64, timeOffset int64) *OrderRevealed {
	pe := randomPreimage()
	return &OrderRevealed{
		&order.MarketOrder{
			P: order.Prefix{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  order.MarketOrderType,
				ClientTime: time.Unix(1566497653+timeOffset, 0),
				ServerTime: time.Unix(1566497656+timeOffset, 0),
				Commit:     pe.Commit(),
			},
			T: order.Trade{
				Coins:    []order.CoinID{},
				Sell:     false,
				Quantity: quantityQuoteAsset,
				Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
			},
		},
		pe,
	}
}

var (
	// Create a coherent order book of standing orders and sorted rates.
	bookBuyOrders = []*order.LimitOrder{
		newLimitOrder(false, 2500000, 2, order.StandingTiF, 0),
		newLimitOrder(false, 2700000, 2, order.StandingTiF, 0),
		newLimitOrder(false, 3200000, 2, order.StandingTiF, 0),
		newLimitOrder(false, 3300000, 1, order.StandingTiF, 2), // newer
		newLimitOrder(false, 3300000, 2, order.StandingTiF, 0), // older
		newLimitOrder(false, 3600000, 4, order.StandingTiF, 0),
		newLimitOrder(false, 3900000, 2, order.StandingTiF, 0),
		newLimitOrder(false, 4000000, 10, order.StandingTiF, 0),
		newLimitOrder(false, 4300000, 4, order.StandingTiF, 1), // newer
		newLimitOrder(false, 4300000, 2, order.StandingTiF, 0), // older
		newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
	}
	bookSellOrders = []*order.LimitOrder{
		newLimitOrder(true, 6200000, 2, order.StandingTiF, 1), // newer
		newLimitOrder(true, 6200000, 2, order.StandingTiF, 0), // older
		newLimitOrder(true, 6100000, 2, order.StandingTiF, 0),
		newLimitOrder(true, 6000000, 2, order.StandingTiF, 0),
		newLimitOrder(true, 5500000, 1, order.StandingTiF, 0),
		newLimitOrder(true, 5400000, 4, order.StandingTiF, 0),
		newLimitOrder(true, 5000000, 2, order.StandingTiF, 0),
		newLimitOrder(true, 4700000, 4, order.StandingTiF, 1),  // newer
		newLimitOrder(true, 4700000, 10, order.StandingTiF, 0), // older
		newLimitOrder(true, 4600000, 2, order.StandingTiF, 0),
		newLimitOrder(true, 4550000, 1, order.StandingTiF, 0),
	}
)

const (
	bookBuyLots   = 32
	bookSellLots  = 32
	initialMidGap = (4550000 + 4500000) / 2 // 4525000
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
		o.FillAmt = 0
	}
	for _, o := range bookSellOrders {
		o.FillAmt = 0
	}
}

func newMatchSet(taker order.Order, makers []*order.LimitOrder, lastPartialAmount ...uint64) *order.MatchSet {
	amounts := make([]uint64, len(makers))
	rates := make([]uint64, len(makers))
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
	return &order.MatchSet{
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
		newLimitOrder(false, 4550000, 1, order.ImmediateTiF, 0), // buy, 1 lot, immediate, equal rate
		newLimitOrder(true, 4450000, 1, order.ImmediateTiF, 0),  // sell, 1 lot, immediate, overlapping rate
		newLimitOrder(true, 4300000, 3, order.StandingTiF, 0),   // sell, 3 lots, immediate, multiple makers
		newLimitOrder(true, 4300000, 2, order.StandingTiF, 0),   // sell, 4 lots, immediate, multiple makers, partial last maker
		newLimitOrder(true, 4300000, 8, order.StandingTiF, 0),   // sell, 8 lots, immediate, multiple makers, partial taker remaining
	}
	resetTakers := func() {
		for _, o := range takers {
			o.FillAmt = 0
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
		wantMatch      *order.MatchSet
		takerRemaining uint64
	}{
		{
			"OK limit buy immediate rate match",
			args{
				book: newBooker(),
				ord:  takers[0],
			},
			true,
			newMatchSet(takers[0], []*order.LimitOrder{bookSellOrders[nSell-1]}),
			0,
		},
		{
			"OK limit sell immediate rate overlap",
			args{
				book: newBooker(),
				ord:  takers[1],
			},
			true,
			newMatchSet(takers[1], []*order.LimitOrder{bookBuyOrders[nBuy-1]}),
			0,
		},
		{
			"OK limit sell immediate multiple makers",
			args{
				book: newBooker(),
				ord:  takers[2],
			},
			true,
			newMatchSet(takers[2], []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2]}),
			0,
		},
		{
			"OK limit sell immediate multiple makers partial maker fill",
			args{
				book: newBooker(),
				ord:  takers[3],
			},
			true,
			newMatchSet(takers[3], []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2]}, 1*LotSize),
			0,
		},
		{
			"OK limit sell immediate multiple makers partial taker fill",
			args{
				book: newBooker(),
				ord:  takers[4],
			},
			true,
			newMatchSet(takers[4], []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2], bookBuyOrders[nBuy-3]}),
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

func newCancelOrder(targetOrderID order.OrderID, serverTime time.Time) *OrderRevealed {
	pe := randomPreimage()
	return &OrderRevealed{
		&order.CancelOrder{
			P: order.Prefix{
				ServerTime: serverTime,
				Commit:     pe.Commit(),
			},
			TargetOrderID: targetOrderID,
		}, pe}
}

func TestMatch_cancelOnly(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := New()

	rand.Seed(1212121)

	fakeOrder := newLimitOrder(false, 4550000, 1, order.ImmediateTiF, 0)
	fakeOrder.ServerTime = time.Now()

	// takers is heterogenous w.r.t. type
	takers := []*OrderRevealed{
		newCancelOrder(bookBuyOrders[3].ID(), fakeOrder.ServerTime.Add(time.Second)),
		newCancelOrder(fakeOrder.ID(), fakeOrder.ServerTime.Add(time.Second)),
	}

	//nSell := len(bookSellOrders)
	//nBuy := len(bookBuyOrders)

	type args struct {
		book  Booker
		queue []*OrderRevealed
	}
	tests := []struct {
		name                   string
		args                   args
		doesMatch              bool
		wantSeed               []byte
		wantMatches            []*order.MatchSet
		wantNumPassed          int
		wantNumFailed          int
		wantDoneOK             int
		wantNumPartial         int
		wantNumInserted        int
		wantNumUnbooked        int
		wantNumCancelsExecuted int
		wantNumTradesCanceled  int
		wantNumNomatched       int
	}{
		{
			name: "cancel standing ok",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{takers[0]},
			},
			doesMatch: true,
			wantSeed: []byte{
				0xbb, 0x20, 0x85, 0x39, 0x78, 0xd5, 0x09, 0xa9,
				0xa0, 0x56, 0x78, 0x4c, 0x50, 0xb1, 0xb4, 0x3d,
				0xe3, 0xe9, 0x9b, 0x2b, 0x88, 0x35, 0x2c, 0x71,
				0xed, 0xff, 0x5d, 0xc2, 0x1e, 0x87, 0xcb, 0xf8},
			wantMatches: []*order.MatchSet{
				{
					Taker:   takers[0].Order,
					Makers:  []*order.LimitOrder{bookBuyOrders[3]},
					Amounts: []uint64{bookBuyOrders[3].Remaining()},
					Rates:   []uint64{bookBuyOrders[3].Rate},
					Total:   0,
				},
			},
			wantNumPassed:          1,
			wantNumFailed:          0,
			wantDoneOK:             1,
			wantNumPartial:         0,
			wantNumInserted:        0,
			wantNumUnbooked:        1,
			wantNumCancelsExecuted: 1,
			wantNumTradesCanceled:  1,
		},
		{
			name: "cancel non-existent standing",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{takers[1]},
			},
			doesMatch: false,
			wantSeed: []byte{
				0x9a, 0xdb, 0xbb, 0x7a, 0x9f, 0x7f, 0xe1, 0x72,
				0xf0, 0x90, 0x38, 0x7a, 0xa6, 0xb9, 0x8a, 0x83,
				0x5a, 0x75, 0x05, 0x19, 0x66, 0x6d, 0x53, 0x12,
				0x7e, 0xc8, 0x9d, 0xe3, 0x87, 0x5d, 0xdc, 0x8a},
			wantMatches:            nil,
			wantNumPassed:          0,
			wantNumFailed:          1,
			wantDoneOK:             0,
			wantNumPartial:         0,
			wantNumInserted:        0,
			wantNumUnbooked:        0,
			wantNumCancelsExecuted: 0,
			wantNumTradesCanceled:  0,
			wantNumNomatched:       1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetMakers()

			numBuys0 := tt.args.book.BuyCount()

			seed, matches, passed, failed, doneOK, partial, booked, nomatched, unbooked, updates, _ := me.Match(tt.args.book, tt.args.queue)
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
			if !bytes.Equal(seed, tt.wantSeed) {
				t.Errorf("got seed %#v, expected %x", seed, tt.wantSeed)
			}
			if len(passed) != tt.wantNumPassed {
				t.Errorf("number passed %d, expected %d", len(passed), tt.wantNumPassed)
			}
			if len(failed) != tt.wantNumFailed {
				t.Errorf("number failed %d, expected %d", len(failed), tt.wantNumFailed)
			}
			if len(doneOK) != tt.wantDoneOK {
				t.Errorf("number doneOK %d, expected %d", len(doneOK), tt.wantDoneOK)
			}
			if len(partial) != tt.wantNumPartial {
				t.Errorf("number partial %d, expected %d", len(partial), tt.wantNumPartial)
			}
			if len(booked) != tt.wantNumInserted {
				t.Errorf("number booked %d, expected %d", len(booked), tt.wantNumInserted)
			}
			if len(unbooked) != tt.wantNumUnbooked {
				t.Errorf("number unbooked %d, expected %d", len(unbooked), tt.wantNumUnbooked)
			}
			if len(nomatched) != tt.wantNumNomatched {
				t.Errorf("number nomatched %d, expected %d", len(nomatched), tt.wantNumNomatched)
			}

			if len(updates.CancelsExecuted) != tt.wantNumCancelsExecuted {
				t.Errorf("number of cancels executed %d, expected %d", len(updates.CancelsExecuted), tt.wantNumCancelsExecuted)
			}
			if len(updates.TradesCanceled) != tt.wantNumTradesCanceled {
				t.Errorf("number of trades canceled %d, expected %d", len(updates.CancelsExecuted), tt.wantNumCancelsExecuted)
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

	rand.Seed(1212121)

	badLotsizeOrder := newLimit(false, 05000000, 1, order.ImmediateTiF, 0)
	badLotsizeOrder.Order.(*order.LimitOrder).Quantity /= 2

	// takers is heterogenous w.r.t. type
	takers := []*OrderRevealed{
		newLimit(false, 4550000, 1, order.ImmediateTiF, 0), // buy, 1 lot, immediate, equal rate
		newLimit(false, 4550000, 2, order.StandingTiF, 0),  // buy, 2 lot, standing, equal rate, partial taker insert to book
		newLimit(false, 4550000, 2, order.ImmediateTiF, 0), // buy, 2 lot, immediate, equal rate, partial taker unfilled
		newLimit(false, 4100000, 1, order.ImmediateTiF, 0), // buy, 1 lot, immediate, unfilled fail
		newLimit(true, 4540000, 1, order.ImmediateTiF, 0),  // sell, 1 lot, immediate
	}

	// tweak the preimage in takers[4] for the 2-element queue test.
	takers[4].Preimage[0] += 1
	takers[4].Order.(*order.LimitOrder).Commit = takers[4].Preimage.Commit()

	resetTakers := func() {
		for _, o := range takers {
			switch ot := o.Order.(type) {
			case *order.MarketOrder:
				ot.FillAmt = 0
			case *order.LimitOrder:
				ot.FillAmt = 0
			}
		}
	}

	nSell := len(bookSellOrders)

	type args struct {
		book  Booker
		queue []*OrderRevealed
	}
	tests := []struct {
		name                   string
		args                   args
		doesMatch              bool
		wantSeed               []byte
		wantMatches            []*order.MatchSet
		wantNumPassed          int
		wantNumFailed          int
		wantDoneOK             int
		wantNumPartial         int
		wantNumInserted        int
		wantNumUnbooked        int
		wantNumTradesPartial   int
		wantNumTradesBooked    int
		wantNumTradesCanceled  int
		wantNumTradesCompleted int
		wantNumTradesFailed    int
		wantNumNomatched       int
		matchStats             *MatchCycleStats
	}{
		{
			name: "limit buy immediate rate match",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{takers[0]},
			},
			doesMatch: true,
			wantSeed: []byte{
				0xbb, 0x20, 0x85, 0x39, 0x78, 0xd5, 0x09, 0xa9,
				0xa0, 0x56, 0x78, 0x4c, 0x50, 0xb1, 0xb4, 0x3d,
				0xe3, 0xe9, 0x9b, 0x2b, 0x88, 0x35, 0x2c, 0x71,
				0xed, 0xff, 0x5d, 0xc2, 0x1e, 0x87, 0xcb, 0xf8},
			wantMatches: []*order.MatchSet{
				// 1 lot @ 4550000, laves best sell of 4600000
				newMatchSet(takers[0].Order, []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			wantNumPassed:          1,
			wantNumFailed:          0,
			wantDoneOK:             1,
			wantNumPartial:         0,
			wantNumInserted:        0,
			wantNumUnbooked:        1,
			wantNumTradesCompleted: 2,
			wantNumNomatched:       0,
			matchStats: &MatchCycleStats{
				BookBuys:    bookBuyLots * LotSize,
				BookSells:   (bookSellLots - 1) * LotSize,
				MatchVolume: LotSize,
				HighRate:    4550000,
				LowRate:     initialMidGap,
				StartRate:   initialMidGap,
				EndRate:     (4500000 + 4600000) / 2,
			},
		},
		{
			name: "limit buy standing partial taker inserted to book",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{takers[1]},
			},
			doesMatch: true,
			wantSeed: []byte{
				0x9a, 0xdb, 0xbb, 0x7a, 0x9f, 0x7f, 0xe1, 0x72,
				0xf0, 0x90, 0x38, 0x7a, 0xa6, 0xb9, 0x8a, 0x83,
				0x5a, 0x75, 0x05, 0x19, 0x66, 0x6d, 0x53, 0x12,
				0x7e, 0xc8, 0x9d, 0xe3, 0x87, 0x5d, 0xdc, 0x8a},
			wantMatches: []*order.MatchSet{
				// 1 lot @ 4550000. bookvol + 1 - 1 = 0
				newMatchSet(takers[1].Order, []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			wantNumPassed:          1,
			wantNumFailed:          0,
			wantDoneOK:             0,
			wantNumPartial:         1,
			wantNumInserted:        1,
			wantNumUnbooked:        1,
			wantNumTradesBooked:    1,
			wantNumTradesCompleted: 1,
			wantNumNomatched:       0,
			matchStats: &MatchCycleStats{
				BookSells:   (bookSellLots - 1) * LotSize,
				BookBuys:    (bookBuyLots + 1) * LotSize,
				MatchVolume: LotSize,
				HighRate:    4550000,
				LowRate:     initialMidGap,
				StartRate:   initialMidGap,
				EndRate:     (4550000 + 4600000) / 2,
			},
		},
		{
			name: "limit buy immediate partial taker unfilled",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{takers[2]},
			},
			doesMatch: true,
			wantSeed: []byte{
				0x4e, 0xec, 0x23, 0x4f, 0xfd, 0xb1, 0xd8, 0xd9,
				0x14, 0xcd, 0xae, 0x34, 0x3c, 0xa2, 0x14, 0x5d,
				0x6e, 0x2d, 0x92, 0xf3, 0xbf, 0xb2, 0x04, 0xfc,
				0xee, 0x39, 0x13, 0xbb, 0x55, 0xed, 0x98, 0x81},
			wantMatches: []*order.MatchSet{
				// 1 lot @ 4550000. bookvol - 1
				newMatchSet(takers[2].Order, []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			wantNumPassed:          1,
			wantNumFailed:          0,
			wantDoneOK:             1,
			wantNumPartial:         1,
			wantNumInserted:        0,
			wantNumUnbooked:        1,
			wantNumTradesCompleted: 2, // not in partial since they are done
			wantNumNomatched:       0,
			matchStats: &MatchCycleStats{
				BookSells:   (bookSellLots - 1) * LotSize,
				BookBuys:    bookBuyLots * LotSize,
				MatchVolume: LotSize,
				HighRate:    4550000,
				LowRate:     initialMidGap,
				StartRate:   initialMidGap,
				EndRate:     (4500000 + 4600000) / 2,
			},
		},
		{
			name: "limit buy immediate unfilled fail",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{takers[3]},
			},
			doesMatch: false,
			wantSeed: []byte{
				0x6f, 0x3d, 0x0f, 0xc8, 0x33, 0xab, 0x4d, 0xc4,
				0xaf, 0xb7, 0x32, 0xcc, 0xd1, 0x77, 0xe9, 0x3e,
				0x41, 0xdb, 0x0b, 0x7a, 0x9e, 0x6f, 0x34, 0xbe,
				0xc9, 0xab, 0x6b, 0xb5, 0x17, 0xb2, 0x89, 0x82},
			wantMatches:         nil,
			wantNumPassed:       0,
			wantNumFailed:       1,
			wantDoneOK:          0,
			wantNumPartial:      0,
			wantNumInserted:     0,
			wantNumUnbooked:     0,
			wantNumTradesFailed: 1,
			wantNumNomatched:    1,
			matchStats: &MatchCycleStats{
				BookSells: bookSellLots * LotSize,
				BookBuys:  bookBuyLots * LotSize,
				HighRate:  initialMidGap,
				LowRate:   initialMidGap,
				StartRate: initialMidGap,
				EndRate:   initialMidGap,
			},
		},
		{
			name: "bad lot size order",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{badLotsizeOrder},
			},
			doesMatch: false,
			wantSeed: []byte{
				0x76, 0x9b, 0xd2, 0x8a, 0x7c, 0x2d, 0xa2, 0x56,
				0x97, 0xa3, 0xc6, 0x8b, 0x43, 0x0c, 0x9e, 0x9a,
				0xfa, 0x6c, 0xf6, 0x86, 0x70, 0x97, 0xc7, 0x74,
				0xb, 0xfb, 0x29, 0x19, 0xbf, 0x86, 0x6a, 0xc8},
			wantMatches:         nil,
			wantNumPassed:       0,
			wantNumFailed:       1,
			wantDoneOK:          0,
			wantNumPartial:      0,
			wantNumInserted:     0,
			wantNumUnbooked:     0,
			wantNumTradesFailed: 1,
			wantNumNomatched:    0,
		},
		{
			name: "limit buy standing partial taker inserted to book, then filled by down-queue sell",
			args: args{
				book: newBooker(),
				queue: []*OrderRevealed{
					takers[1],
					takers[4],
				},
			},
			doesMatch: true,
			wantSeed: []byte{
				0xb4, 0xc0, 0xcc, 0xc2, 0x3e, 0x50, 0x1d, 0xf5,
				0x15, 0xe1, 0x29, 0xd7, 0x59, 0xd6, 0x7a, 0xb0,
				0xd1, 0x24, 0xfa, 0x3c, 0x9d, 0x37, 0x6e, 0x1a,
				0x8b, 0x15, 0xc5, 0x84, 0xa4, 0xc0, 0x80, 0x98},
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[1].Order, []*order.LimitOrder{bookSellOrders[nSell-1]}),
				{ // the maker is reduced by matching first item in the queue
					Taker:   takers[4].Order,
					Makers:  []*order.LimitOrder{takers[1].Order.(*order.LimitOrder)},
					Amounts: []uint64{1 * LotSize}, // 2 - 1
					Rates:   []uint64{4550000},
					Total:   1 * LotSize,
				},
			},
			wantNumPassed:          2,
			wantNumFailed:          0,
			wantDoneOK:             1,
			wantNumPartial:         1,
			wantNumInserted:        1,
			wantNumUnbooked:        2,
			wantNumTradesBooked:    1,
			wantNumTradesCompleted: 3, // both queue orders and one book sell order fully filled
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetTakers()
			resetMakers()

			seed, matches, passed, failed, doneOK, partial, booked, nomatched, unbooked, updates, stats := me.Match(tt.args.book, tt.args.queue)
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
			if !bytes.Equal(seed, tt.wantSeed) {
				t.Errorf("got seed %#v, expected %x", seed, tt.wantSeed)
			}
			if len(passed) != tt.wantNumPassed {
				t.Errorf("number passed %d, expected %d", len(passed), tt.wantNumPassed)
			}
			if len(failed) != tt.wantNumFailed {
				t.Errorf("number failed %d, expected %d", len(failed), tt.wantNumFailed)
			}
			if len(doneOK) != tt.wantDoneOK {
				t.Errorf("number doneOK %d, expected %d", len(doneOK), tt.wantDoneOK)
			}
			if len(partial) != tt.wantNumPartial {
				t.Errorf("number partial %d, expected %d", len(partial), tt.wantNumPartial)
			}
			if len(booked) != tt.wantNumInserted {
				t.Errorf("number booked %d, expected %d", len(booked), tt.wantNumInserted)
			}
			if len(unbooked) != tt.wantNumUnbooked {
				t.Errorf("number unbooked %d, expected %d", len(unbooked), tt.wantNumUnbooked)
			}

			if len(updates.TradesCompleted) != tt.wantNumTradesCompleted {
				t.Errorf("number of trades completed %d, expected %d", len(updates.TradesCompleted), tt.wantNumTradesCompleted)
			}
			if len(updates.TradesBooked) != tt.wantNumTradesBooked {
				t.Errorf("number of trades booked %d, expected %d", len(updates.TradesBooked), tt.wantNumTradesBooked)
			}
			if len(updates.TradesFailed) != tt.wantNumTradesFailed {
				t.Errorf("number of trades failed %d, expected %d", len(updates.TradesFailed), tt.wantNumTradesFailed)
			}
			if len(nomatched) != tt.wantNumNomatched {
				t.Errorf("number of trades nomatched %d, expected %d", len(nomatched), tt.wantNumNomatched)
			}
			if tt.matchStats != nil {
				compareMatchStats(t, tt.matchStats, stats)
			}
		})
	}
}

func TestMatch_marketSellsOnly(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := New()

	rand.Seed(1212121)

	badLotsizeOrder := newMarketSellOrder(1, 0)
	badLotsizeOrder.Order.(*order.MarketOrder).Quantity /= 2

	// takers is heterogenous w.r.t. type
	takers := []*OrderRevealed{
		newMarketSellOrder(1, 0),  // sell, 1 lot
		newMarketSellOrder(3, 0),  // sell, 3 lot
		newMarketSellOrder(6, 0),  // sell, 6 lot, partial maker fill
		newMarketSellOrder(99, 0), // sell, 99 lot, partial taker fill
	}

	partialRate := uint64(2500000)
	partialWithRemainder := newLimitOrder(false, partialRate, 2, order.StandingTiF, 0)
	partialThenGone := newLimitOrder(false, partialRate, 4, order.StandingTiF, 0)

	resetTakers := func() {
		for _, o := range takers {
			switch ot := o.Order.(type) {
			case *order.MarketOrder:
				ot.FillAmt = 0
			case *order.LimitOrder:
				ot.FillAmt = 0
			}
		}
	}

	nBuy := len(bookBuyOrders)

	type args struct {
		book  Booker
		queue []*OrderRevealed
	}
	tests := []struct {
		name                   string
		args                   args
		doesMatch              bool
		wantSeed               []byte
		wantMatches            []*order.MatchSet
		wantNumPassed          int
		wantNumFailed          int
		wantDoneOK             int
		wantNumPartial         int
		wantNumInserted        int
		wantNumUnbooked        int
		wantNumTradesCompleted int
		wantNumTradesPartial   int
		wantNumTradesFailed    int
	}{
		{
			name: "market sell, 1 maker match",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{takers[0]},
			},
			doesMatch: true,
			wantSeed: []byte{
				0xbb, 0x20, 0x85, 0x39, 0x78, 0xd5, 0x09, 0xa9,
				0xa0, 0x56, 0x78, 0x4c, 0x50, 0xb1, 0xb4, 0x3d,
				0xe3, 0xe9, 0x9b, 0x2b, 0x88, 0x35, 0x2c, 0x71,
				0xed, 0xff, 0x5d, 0xc2, 0x1e, 0x87, 0xcb, 0xf8},
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[0].Order, []*order.LimitOrder{bookBuyOrders[nBuy-1]}),
			},
			wantNumPassed:          1,
			wantNumFailed:          0,
			wantDoneOK:             1,
			wantNumPartial:         0,
			wantNumInserted:        0,
			wantNumUnbooked:        1,
			wantNumTradesCompleted: 2, // queued market and already booked limit
		},
		{
			name: "market sell, 2 maker match",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{takers[1]},
			},
			doesMatch: true,
			wantSeed: []byte{
				0x9a, 0xdb, 0xbb, 0x7a, 0x9f, 0x7f, 0xe1, 0x72,
				0xf0, 0x90, 0x38, 0x7a, 0xa6, 0xb9, 0x8a, 0x83,
				0x5a, 0x75, 0x05, 0x19, 0x66, 0x6d, 0x53, 0x12,
				0x7e, 0xc8, 0x9d, 0xe3, 0x87, 0x5d, 0xdc, 0x8a},
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[1].Order, []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2]}),
			},
			wantNumPassed:          1,
			wantNumFailed:          0,
			wantDoneOK:             1,
			wantNumPartial:         0,
			wantNumInserted:        0,
			wantNumUnbooked:        2,
			wantNumTradesCompleted: 3, // queued market and two already booked limits
		},
		{
			name: "market sell, 3 maker match, 1 partial maker fill",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{takers[2]},
			},
			doesMatch: true,
			wantSeed: []byte{
				0x4e, 0xec, 0x23, 0x4f, 0xfd, 0xb1, 0xd8, 0xd9,
				0x14, 0xcd, 0xae, 0x34, 0x3c, 0xa2, 0x14, 0x5d,
				0x6e, 0x2d, 0x92, 0xf3, 0xbf, 0xb2, 0x04, 0xfc,
				0xee, 0x39, 0x13, 0xbb, 0x55, 0xed, 0x98, 0x81},
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[2].Order, []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2], bookBuyOrders[nBuy-3]}, 3*LotSize),
			},
			wantNumPassed:          1,
			wantNumFailed:          0,
			wantDoneOK:             1,
			wantNumPartial:         0,
			wantNumInserted:        0,
			wantNumUnbooked:        2,
			wantNumTradesCompleted: 3, // taker and two already booked limits
			wantNumTradesPartial:   1, // partial fill of one already booked limit, which consumed the taker
		},
		{
			name: "market sell bad lot size",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{badLotsizeOrder},
			},
			doesMatch: false,
			wantSeed: []byte{
				0x76, 0x9b, 0xd2, 0x8a, 0x7c, 0x2d, 0xa2, 0x56,
				0x97, 0xa3, 0xc6, 0x8b, 0x43, 0x0c, 0x9e, 0x9a,
				0xfa, 0x6c, 0xf6, 0x86, 0x70, 0x97, 0xc7, 0x74,
				0x0b, 0xfb, 0x29, 0x19, 0xbf, 0x86, 0x6a, 0xc8},
			wantMatches:         nil,
			wantNumPassed:       0,
			wantNumFailed:       1,
			wantDoneOK:          0,
			wantNumPartial:      0,
			wantNumInserted:     0,
			wantNumUnbooked:     0,
			wantNumTradesFailed: 1,
		},
		{
			name: "market sell partial fill with remainder",
			args: args{
				book: &BookStub{
					lotSize:   LotSize,
					buyOrders: []*order.LimitOrder{partialWithRemainder},
				},
				queue: []*OrderRevealed{takers[0]},
			},
			doesMatch: true,
			wantSeed: []byte{
				0xbb, 0x20, 0x85, 0x39, 0x78, 0xd5, 0x09, 0xa9,
				0xa0, 0x56, 0x78, 0x4c, 0x50, 0xb1, 0xb4, 0x3d,
				0xe3, 0xe9, 0x9b, 0x2b, 0x88, 0x35, 0x2c, 0x71,
				0xed, 0xff, 0x5d, 0xc2, 0x1e, 0x87, 0xcb, 0xf8},
			wantMatches: []*order.MatchSet{
				{
					Taker:   takers[0].Order,
					Makers:  []*order.LimitOrder{partialWithRemainder},
					Amounts: []uint64{LotSize},
					Rates:   []uint64{partialRate},
					Total:   LotSize,
				},
			},
			wantNumPassed:          1,
			wantNumFailed:          0,
			wantDoneOK:             1,
			wantNumTradesPartial:   1,
			wantNumTradesCompleted: 1,
			wantNumUnbooked:        0,
			wantNumTradesFailed:    0,
		},
		// If a booked order partially matches, but then completes on a subsequent
		// order, the updates.TradesPartial should not contain the order.
		{
			name: "market sell partial fill before complete fill",
			args: args{
				book: &BookStub{
					lotSize:   LotSize,
					buyOrders: []*order.LimitOrder{partialThenGone},
				},
				queue: []*OrderRevealed{takers[0], takers[1]},
			},
			doesMatch: true,
			wantSeed: []byte{
				0x29, 0x39, 0xf3, 0x0e, 0x9e, 0x00, 0xc6, 0xe5,
				0xc7, 0x1d, 0x56, 0x82, 0x2c, 0x89, 0x92, 0x3c,
				0x59, 0x97, 0x84, 0x43, 0xaf, 0x7f, 0x7b, 0x67,
				0xeb, 0x54, 0xd0, 0x88, 0x01, 0x2b, 0xf0, 0x58},
			wantMatches: []*order.MatchSet{
				{
					Taker:   takers[0].Order,
					Makers:  []*order.LimitOrder{partialThenGone},
					Amounts: []uint64{LotSize},
					Rates:   []uint64{partialRate},
					Total:   LotSize,
				},
				{
					Taker:   takers[1].Order,
					Makers:  []*order.LimitOrder{partialThenGone},
					Amounts: []uint64{LotSize * 3},
					Rates:   []uint64{partialRate},
					Total:   LotSize * 3,
				},
			},
			wantNumPassed:          2,
			wantNumFailed:          0,
			wantDoneOK:             2,
			wantNumTradesPartial:   0,
			wantNumTradesCompleted: 3,
			wantNumUnbooked:        1,
			wantNumTradesFailed:    0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetTakers()
			resetMakers()

			fmt.Printf("%v\n", takers)

			seed, matches, passed, failed, doneOK, partial, booked, _, unbooked, updates, _ := me.Match(tt.args.book, tt.args.queue)
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
			if !bytes.Equal(seed, tt.wantSeed) {
				t.Errorf("got seed %x, expected %x", seed, tt.wantSeed)
			}
			if len(passed) != tt.wantNumPassed {
				t.Errorf("number passed %d, expected %d", len(passed), tt.wantNumPassed)
			}
			if len(failed) != tt.wantNumFailed {
				t.Errorf("number failed %d, expected %d", len(failed), tt.wantNumFailed)
			}
			if len(doneOK) != tt.wantDoneOK {
				t.Errorf("number doneOK %d, expected %d", len(doneOK), tt.wantDoneOK)
			}
			if len(partial) != tt.wantNumPartial {
				t.Errorf("number partial %d, expected %d", len(partial), tt.wantNumPartial)
			}
			if len(booked) != tt.wantNumInserted {
				t.Errorf("number booked %d, expected %d", len(booked), tt.wantNumInserted)
			}
			if len(unbooked) != tt.wantNumUnbooked {
				t.Errorf("number unbooked %d, expected %d", len(unbooked), tt.wantNumUnbooked)
			}

			if len(updates.TradesCompleted) != tt.wantNumTradesCompleted {
				t.Errorf("number of trades completed %d, expected %d", len(updates.TradesCompleted), tt.wantNumTradesCompleted)
			}
			if len(updates.TradesPartial) != tt.wantNumTradesPartial {
				t.Errorf("number of book trades partially filled %d, expected %d", len(updates.TradesPartial), tt.wantNumTradesPartial)
			}
			if len(updates.TradesFailed) != tt.wantNumTradesFailed {
				t.Errorf("number of trades failed %d, expected %d", len(updates.TradesFailed), tt.wantNumTradesFailed)
			}
			t.Log(updates)
		})
	}
}

// marketBuyQuoteAmt gives the exact amount in the quote asset require to
// purchase lots worth of the base asset given the current sell order book.
func marketBuyQuoteAmt(lots uint64) uint64 {
	var amt uint64
	var i int
	nSell := len(bookSellOrders)
	for lots > 0 && i < nSell {
		sellOrder := bookSellOrders[nSell-1-i]
		orderLots := sellOrder.Quantity / LotSize
		if orderLots > lots {
			orderLots = lots
		}
		lots -= orderLots

		amt += BaseToQuote(sellOrder.Rate, orderLots*LotSize)
		i++
	}
	return amt
}

// quoteAmt computes the required amount of the quote asset required to purchase
// the specified number of lots given the current order book and required amount
// buffering in the single lot case.
func quoteAmt(lots uint64) uint64 {
	amt := marketBuyQuoteAmt(lots)
	if lots == 1 {
		amt *= 3
		amt /= 2
	}
	return amt
}

func TestMatch_marketBuysOnly(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := New()

	rand.Seed(1212121)

	nSell := len(bookSellOrders)

	// takers is heterogenous w.r.t. type
	takers := []*OrderRevealed{
		newMarketBuyOrder(quoteAmt(1), 0),  // buy, 1 lot
		newMarketBuyOrder(quoteAmt(2), 0),  // buy, 2 lot
		newMarketBuyOrder(quoteAmt(3), 0),  // buy, 3 lot
		newMarketBuyOrder(quoteAmt(99), 0), // buy, 99 lot
	}

	resetTakers := func() {
		for _, o := range takers {
			switch ot := o.Order.(type) {
			case *order.MarketOrder:
				ot.FillAmt = 0
			case *order.LimitOrder:
				ot.FillAmt = 0
			}
		}
	}

	bookSellOrdersReverse := make([]*order.LimitOrder, len(bookSellOrders))
	for i := range bookSellOrders {
		bookSellOrdersReverse[len(bookSellOrders)-1-i] = bookSellOrders[i]
	}

	type args struct {
		book  Booker
		queue []*OrderRevealed
	}
	tests := []struct {
		name                   string
		args                   args
		doesMatch              bool
		wantSeed               []byte
		wantMatches            []*order.MatchSet
		remaining              []uint64
		wantNumPassed          int
		wantNumFailed          int
		wantDoneOK             int
		wantNumPartial         int
		wantNumInserted        int
		wantNumUnbooked        int
		wantNumTradesCompleted int
		wantNumTradesPartial   int
		wantNumTradesFailed    int
		matchStats             *MatchCycleStats
	}{
		{
			name: "market buy, 1 maker match",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{takers[0]},
			},
			doesMatch: true,
			wantSeed: []byte{
				0x76, 0x9b, 0xd2, 0x8a, 0x7c, 0x2d, 0xa2, 0x56,
				0x97, 0xa3, 0xc6, 0x8b, 0x43, 0x0c, 0x9e, 0x9a,
				0xfa, 0x6c, 0xf6, 0x86, 0x70, 0x97, 0xc7, 0x74,
				0x0b, 0xfb, 0x29, 0x19, 0xbf, 0x86, 0x6a, 0xc8},
			wantMatches: []*order.MatchSet{
				// 1 lot @ 4550000, leaving best sell as 4600000
				newMatchSet(takers[0].Order, []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			remaining:              []uint64{quoteAmt(1) - marketBuyQuoteAmt(1)},
			wantNumPassed:          1,
			wantNumFailed:          0,
			wantDoneOK:             1,
			wantNumPartial:         0,
			wantNumInserted:        0,
			wantNumUnbooked:        1,
			wantNumTradesCompleted: 2, // the taker and the maker are completed
			matchStats: &MatchCycleStats{
				BookSells:   (bookSellLots - 1) * LotSize,
				BookBuys:    bookBuyLots * LotSize,
				MatchVolume: LotSize,
				HighRate:    4550000,
				LowRate:     initialMidGap,
				StartRate:   initialMidGap,
				EndRate:     (4500000 + 4600000) / 2,
			},
		},
		{
			name: "market buy, 2 maker match",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{takers[1]},
			},
			doesMatch: true,
			wantSeed: []byte{
				0xbb, 0x20, 0x85, 0x39, 0x78, 0xd5, 0x09, 0xa9,
				0xa0, 0x56, 0x78, 0x4c, 0x50, 0xb1, 0xb4, 0x3d,
				0xe3, 0xe9, 0x9b, 0x2b, 0x88, 0x35, 0x2c, 0x71,
				0xed, 0xff, 0x5d, 0xc2, 0x1e, 0x87, 0xcb, 0xf8},
			wantMatches: []*order.MatchSet{
				// 1 lot @ 4550000, 1 @ 4600000
				newMatchSet(takers[1].Order, []*order.LimitOrder{bookSellOrders[nSell-1], bookSellOrders[nSell-2]}, 1*LotSize),
			},
			remaining:              []uint64{0},
			wantNumPassed:          1,
			wantNumFailed:          0,
			wantDoneOK:             1,
			wantNumPartial:         0,
			wantNumInserted:        0,
			wantNumUnbooked:        1,
			wantNumTradesCompleted: 2, // taker, and one maker
			wantNumTradesPartial:   1, // the second maker
			matchStats: &MatchCycleStats{
				BookSells:   (bookSellLots - 2) * LotSize,
				BookBuys:    bookBuyLots * LotSize,
				MatchVolume: 2 * LotSize,
				HighRate:    4600000,
				LowRate:     initialMidGap,
				StartRate:   initialMidGap,
				EndRate:     (4500000 + 4600000) / 2,
			},
		},
		{
			name: "market buy, 3 maker match",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{takers[2]},
			},
			doesMatch: true,
			wantSeed: []byte{
				0x9a, 0xdb, 0xbb, 0x7a, 0x9f, 0x7f, 0xe1, 0x72,
				0xf0, 0x90, 0x38, 0x7a, 0xa6, 0xb9, 0x8a, 0x83,
				0x5a, 0x75, 0x05, 0x19, 0x66, 0x6d, 0x53, 0x12,
				0x7e, 0xc8, 0x9d, 0xe3, 0x87, 0x5d, 0xdc, 0x8a},
			wantMatches: []*order.MatchSet{
				// 1 @ 4550000, 2 @ 4600000, leaves best sell of 4700000 behind/
				newMatchSet(takers[2].Order, []*order.LimitOrder{bookSellOrders[nSell-1], bookSellOrders[nSell-2]}),
			},
			remaining:              []uint64{0},
			wantNumPassed:          1,
			wantNumFailed:          0,
			wantDoneOK:             1,
			wantNumPartial:         0,
			wantNumInserted:        0,
			wantNumUnbooked:        2,
			wantNumTradesCompleted: 3, // taker, 2 makers
			matchStats: &MatchCycleStats{
				BookSells:   (bookSellLots - 3) * LotSize,
				BookBuys:    bookBuyLots * LotSize,
				MatchVolume: 3 * LotSize,
				HighRate:    4600000,
				LowRate:     initialMidGap,
				StartRate:   initialMidGap,
				EndRate:     (4500000 + 4700000) / 2,
			},
		},
		{
			name: "market buy, 99 maker match",
			args: args{
				book:  newBooker(),
				queue: []*OrderRevealed{takers[3]},
			},
			doesMatch: true,
			wantSeed: []byte{
				0x4e, 0xec, 0x23, 0x4f, 0xfd, 0xb1, 0xd8, 0xd9,
				0x14, 0xcd, 0xae, 0x34, 0x3c, 0xa2, 0x14, 0x5d,
				0x6e, 0x2d, 0x92, 0xf3, 0xbf, 0xb2, 0x04, 0xfc,
				0xee, 0x39, 0x13, 0xbb, 0x55, 0xed, 0x98, 0x81},
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[3].Order, bookSellOrdersReverse),
			},
			remaining:              []uint64{0},
			wantNumPassed:          1,
			wantNumFailed:          0,
			wantDoneOK:             1,
			wantNumPartial:         0,
			wantNumInserted:        0,
			wantNumUnbooked:        11,
			wantNumTradesCompleted: 12, // taker, 11 makers
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetTakers()
			resetMakers()

			seed, matches, passed, failed, doneOK, partial, booked, _, unbooked, updates, stats := me.Match(tt.args.book, tt.args.queue)
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
				if matches[i].Taker.Trade().Remaining() != tt.remaining[i] {
					t.Errorf("Incorrect taker order amount remaining. Expected %d, got %d",
						tt.remaining[i], matches[i].Taker.Trade().Remaining())
				}
			}
			if !bytes.Equal(seed, tt.wantSeed) {
				t.Errorf("got seed %x, expected %x", seed, tt.wantSeed)
			}
			if len(passed) != tt.wantNumPassed {
				t.Errorf("number passed %d, expected %d", len(passed), tt.wantNumPassed)
			}
			if len(failed) != tt.wantNumFailed {
				t.Errorf("number failed %d, expected %d", len(failed), tt.wantNumFailed)
			}
			if len(doneOK) != tt.wantDoneOK {
				t.Errorf("number doneOK %d, expected %d", len(doneOK), tt.wantDoneOK)
			}
			if len(partial) != tt.wantNumPartial {
				t.Errorf("number partial %d, expected %d", len(partial), tt.wantNumPartial)
			}
			if len(booked) != tt.wantNumInserted {
				t.Errorf("number booked %d, expected %d", len(booked), tt.wantNumInserted)
			}
			if len(unbooked) != tt.wantNumUnbooked {
				t.Errorf("number unbooked %d, expected %d", len(unbooked), tt.wantNumUnbooked)
			}

			if len(updates.TradesCompleted) != tt.wantNumTradesCompleted {
				t.Errorf("number of trades completed %d, expected %d", len(updates.TradesCompleted), tt.wantNumTradesCompleted)
			}
			if len(updates.TradesPartial) != tt.wantNumTradesPartial {
				t.Errorf("number of book trades partially filled %d, expected %d", len(updates.TradesPartial), tt.wantNumTradesPartial)
			}
			if len(updates.TradesFailed) != tt.wantNumTradesFailed {
				t.Errorf("number of trades failed %d, expected %d", len(updates.TradesFailed), tt.wantNumTradesFailed)
			}
			if tt.matchStats != nil {
				compareMatchStats(t, tt.matchStats, stats)
			}
		})
	}
}
func Test_shuffleQueue(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// order queues to be shuffled
	q3_1 := []*OrderRevealed{
		limitOrders[0],
		marketOrders[0],
		marketOrders[1],
	}

	// q3_2 has same orders as q1 in different order
	q3_2 := []*OrderRevealed{
		marketOrders[0],
		limitOrders[0],
		marketOrders[1],
	}

	// q3Shuffled is the expected result of sorting q3_1 and q3_2
	q3Shuffled := []*OrderRevealed{
		marketOrders[1],
		limitOrders[0],
		marketOrders[0],
	}
	q3Seed, _ := hex.DecodeString("18ad6e0777c50ce4ea64efcc1a39b8aea26bc96f78ebf163470e0495edd9fb80")

	// shuffleQueue should work with nil slice
	var qNil []*OrderRevealed

	// shuffleQueue should work with empty slice
	qEmpty := []*OrderRevealed{}

	// shuffleQueue should work with single element slice
	q1 := []*OrderRevealed{marketOrders[0]}
	q1Seed, _ := hex.DecodeString("d5546f7fbbc4c7761c9a0fa986956f28208d55cb0bf2e132eae01362f51ee1ed")

	// shuffleQueue should work with two element slice
	q2_a := []*OrderRevealed{
		limitOrders[0],
		marketOrders[0],
	}

	// ... with same output regardless of input order
	q2_b := []*OrderRevealed{
		marketOrders[0],
		limitOrders[0],
	}

	q2Shuffled := []*OrderRevealed{
		marketOrders[0],
		limitOrders[0],
	}
	q2Seed, _ := hex.DecodeString("fe51266c189dcc2f5413b62f5e7ef5015bb0d7305188813c5881c1747996ad6b")

	tests := []struct {
		name     string
		inOut    []*OrderRevealed
		want     []*OrderRevealed
		wantSeed []byte
	}{
		{
			"q3_1 iter 1",
			q3_1,
			q3Shuffled,
			q3Seed,
		},
		{
			"q3_1 iter 2",
			q3_1,
			q3Shuffled,
			q3Seed,
		},
		{
			"q3_1 iter 3",
			q3_1,
			q3Shuffled,
			q3Seed,
		},
		{
			"q3_2",
			q3_2,
			q3Shuffled,
			q3Seed,
		},
		{
			"qEmpty",
			qEmpty,
			[]*OrderRevealed{},
			[]byte{},
		},
		{
			"qNil",
			qNil,
			[]*OrderRevealed(nil),
			[]byte{},
		},
		{
			"q1",
			q1,
			[]*OrderRevealed{q1[0]},
			q1Seed,
		},
		{
			"q2_a",
			q2_a,
			q2Shuffled,
			q2Seed,
		},
		{
			"q2_b",
			q2_b,
			q2Shuffled,
			q2Seed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seed := shuffleQueue(tt.inOut)
			if !reflect.DeepEqual(tt.inOut, tt.want) {
				t.Errorf("shuffleQueue(q): q = %#v, want %#v", tt.inOut, tt.want)
			}
			if !bytes.Equal(seed, tt.wantSeed) {
				t.Errorf("got seed %x, expected %x", seed, tt.wantSeed)
			}
		})
	}
}

func Test_sortQueue(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// order queues to be sorted
	q3_1 := []*OrderRevealed{
		limitOrders[0],  // df8b93e65c03a7ec20f8ae8dce42ba132a613ec3d3848900209d46f515f2d46e
		marketOrders[0], // d793e1b4e34d86800ce6a207478d17b4cba73b2b65987b293a361b971697d45c
		marketOrders[1], // edf28bf4dc05fce563b1c2725aecec5edef5098c3a9d76fc71d6473ee2dc3fa5
	}

	// q3_2 has same orders as q1 in different order
	q3_2 := []*OrderRevealed{
		marketOrders[0],
		limitOrders[0],
		marketOrders[1],
	}

	// q3Sorted is the expected result of sorting q3_1 and q3_2
	q3Sorted := []*OrderRevealed{
		marketOrders[0], // #1: d793e1b4e34d86800ce6a207478d17b4cba73b2b65987b293a361b971697d45c
		limitOrders[0],  // #2: df8b93e65c03a7ec20f8ae8dce42ba132a613ec3d3848900209d46f515f2d46e
		marketOrders[1], // #3: edf28bf4dc05fce563b1c2725aecec5edef5098c3a9d76fc71d6473ee2dc3fa5
	}

	// sortQueue should work with nil slice
	var qNil []*OrderRevealed

	// sortQueue should work with empty slice
	qEmpty := []*OrderRevealed{}

	// sortQueue should work with single element slice
	q1 := []*OrderRevealed{marketOrders[0]}

	// sortQueue should work with two element slice
	q2_a := []*OrderRevealed{
		limitOrders[0],
		marketOrders[0],
	}

	// ... with same output regardless of input order
	q2_b := []*OrderRevealed{
		marketOrders[0],
		limitOrders[0],
	}

	q2Sorted := []*OrderRevealed{
		marketOrders[0],
		limitOrders[0],
	}

	tests := []struct {
		name  string
		inOut []*OrderRevealed
		want  []*OrderRevealed
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
			[]*OrderRevealed{},
		},
		{
			"qNil",
			qNil,
			[]*OrderRevealed(nil),
		},
		{
			"q1",
			q1,
			[]*OrderRevealed{q1[0]},
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortQueueByID(tt.inOut)
			if !reflect.DeepEqual(tt.inOut, tt.want) {
				t.Errorf("sortQueueByID(q): q = %#v, want %#v", tt.inOut, tt.want)
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
				marketOrders[0].Order,
				limitOrders[1].Order,
			},
			true,
		},
		{
			"MATCH market sell : limit buy",
			args{
				marketOrders[1].Order,
				limitOrders[0].Order,
			},
			true,
		},
		{
			"MATCH limit sell : market buy",
			args{
				limitOrders[1].Order,
				marketOrders[0].Order,
			},
			true,
		},
		{
			"MATCH limit buy : market sell",
			args{
				limitOrders[0].Order,
				marketOrders[1].Order,
			},
			true,
		},
		{
			"NO MATCH market sell : market buy",
			args{
				marketOrders[1].Order,
				marketOrders[0].Order,
			},
			false,
		},
		{
			"NO MATCH (rates) limit sell : limit buy",
			args{
				limitOrders[0].Order,
				limitOrders[1].Order,
			},
			false,
		},
		{
			"NO MATCH (rates) limit buy : limit sell",
			args{
				limitOrders[1].Order,
				limitOrders[0].Order,
			},
			false,
		},
		{
			"MATCH (overlapping rates) limit sell : limit buy",
			args{
				limitOrders[1].Order,
				limitOrders[2].Order,
			},
			true,
		},
		{
			"MATCH (same rates) limit sell : limit buy",
			args{
				limitOrders[1].Order,
				limitOrders[3].Order,
			},
			true,
		},
		{
			"NO MATCH (same side) limit buy : limit buy",
			args{
				limitOrders[2].Order,
				limitOrders[3].Order,
			},
			false,
		},
		{
			"NO MATCH (cancel) market buy : cancel",
			args{
				marketOrders[0].Order,
				&order.CancelOrder{},
			},
			false,
		},
		{
			"NO MATCH (cancel) cancel : market sell",
			args{
				&order.CancelOrder{},
				marketOrders[1].Order,
			},
			false,
		},
		{
			"NO MATCH (cancel) limit sell : cancel",
			args{
				limitOrders[1].Order,
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

func TestBaseToQuote(t *testing.T) {
	type args struct {
		rate      uint64
		rateFloat float64
		base      uint64
	}
	tests := []struct {
		name      string
		args      args
		wantQuote uint64
	}{
		{
			name: "ok <1",
			args: args{
				rate:      1234132,
				rateFloat: 0.01234132,
				base:      4200000000,
			},
			wantQuote: 51833544,
		},
		{
			name: "ok 1",
			args: args{
				rate:      100000000,
				rateFloat: 1.0,
				base:      4200000000,
			},
			wantQuote: 4200000000,
		},
		{
			name: "ok >1",
			args: args{
				rate:      100000022,
				rateFloat: 1.00000022,
				base:      4200000000,
			},
			wantQuote: 4200000924,
		},
		{
			name: "ok >>1",
			args: args{
				rate:      19900000022,
				rateFloat: 199.00000022,
				base:      4200000000,
			},
			wantQuote: 835800000924,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotQuote := BaseToQuote(tt.args.rate, tt.args.base)
			if gotQuote != tt.wantQuote {
				t.Errorf("BaseToQuote() = %v, want %v", gotQuote, tt.wantQuote)
			}
			// quote2 := uint64(tt.args.rateFloat * float64(tt.args.base))
			// t.Logf("quote from integer rate = %d, from float rate = %d, diff = %d",
			// 	gotQuote, quote2, int64(gotQuote-quote2))
		})
	}
}

func TestQuoteToBase(t *testing.T) {
	type args struct {
		rate      uint64
		rateFloat float64
		quote     uint64
	}
	tests := []struct {
		name     string
		args     args
		wantBase uint64
	}{
		{
			name: "ok <1",
			args: args{
				rate:      1234132,
				rateFloat: 0.01234132,
				quote:     51833544,
			},
			wantBase: 4200000000,
		},
		{
			name: "ok 1",
			args: args{
				rate:      100000000,
				rateFloat: 1.0,
				quote:     4200000000,
			},
			wantBase: 4200000000,
		},
		{
			name: "ok >1",
			args: args{
				rate:      100000022,
				rateFloat: 1.00000022,
				quote:     4200000924,
			},
			wantBase: 4200000000,
		},
		{
			name: "don't panic on 0 rate",
			args: args{
				rate:      0,
				rateFloat: 0,
				quote:     51833544,
			},
			wantBase: 0,
		},
		{
			name: "ok >>1",
			args: args{
				rate:      19900000022,
				rateFloat: 199.00000022,
				quote:     835800000924,
			},
			wantBase: 4200000000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBase := QuoteToBase(tt.args.rate, tt.args.quote)
			if gotBase != tt.wantBase {
				t.Errorf("QuoteToBase() = %v, want %v", gotBase, tt.wantBase)
			}
			// base2 := uint64(float64(tt.args.quote) / tt.args.rateFloat)
			// t.Logf("base2 from integer rate = %d, from float rate = %d, diff = %d",
			// 	gotBase, base2, int64(gotBase-base2))
		})
	}
}

func TestQuoteToBaseToQuote(t *testing.T) {
	type args struct {
		rate      uint64
		rateFloat float64
		quote     uint64
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "ok <1",
			args: args{
				rate:      1234132,
				rateFloat: 0.01234132,
				quote:     51833544,
			},
		},
		{
			name: "ok 1",
			args: args{
				rate:      100000000,
				rateFloat: 1.0,
				quote:     4200000000,
			},
		},
		{
			name: "ok >1",
			args: args{
				rate:      100000022,
				rateFloat: 1.00000022,
				quote:     4200000924,
			},
		},
		{
			name: "ok >>1",
			args: args{
				rate:      19900000022,
				rateFloat: 199.00000022,
				quote:     835800000924,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBase := QuoteToBase(tt.args.rate, tt.args.quote)
			gotQuote := BaseToQuote(tt.args.rate, gotBase)
			if gotQuote != tt.args.quote {
				t.Errorf("Failed quote->base->quote round trip. %d != %d",
					gotQuote, tt.args.quote)
			}

			// baseFlt := float64(tt.args.quote) / tt.args.rateFloat
			// quoteFlt := baseFlt * tt.args.rateFloat

			// t.Logf("expected quote = %d, final quote = %d, float quote = %f",
			// 	tt.args.quote, gotQuote, quoteFlt)
		})
	}
}

func TestBaseToQuoteToBase(t *testing.T) {
	type args struct {
		rate      uint64
		rateFloat float64
		base      uint64
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "ok <1",
			args: args{
				rate:      1234132,
				rateFloat: 0.01234132,
				base:      4200000000,
			},
		},
		{
			name: "ok 1",
			args: args{
				rate:      100000000,
				rateFloat: 1.0,
				base:      4200000000,
			},
		},
		{
			name: "ok >1",
			args: args{
				rate:      100000022,
				rateFloat: 1.00000022,
				base:      4200000000,
			},
		},
		{
			name: "ok >>1",
			args: args{
				rate:      19900000022,
				rateFloat: 199.00000022,
				base:      4200000000,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotQuote := BaseToQuote(tt.args.rate, tt.args.base)
			gotBase := QuoteToBase(tt.args.rate, gotQuote)
			if gotBase != tt.args.base {
				t.Errorf("Failed base->quote->base round trip. %d != %d",
					gotBase, tt.args.base)
			}

			// quoteFlt := float64(tt.args.base) * tt.args.rateFloat
			// baseFlt := quoteFlt / tt.args.rateFloat

			// t.Logf("expected quote = %d, final quote = %d, float quote = %f",
			// 	tt.args.base, gotBase, baseFlt)
		})
	}
}

func compareMatchStats(t *testing.T, sWant, sHave *MatchCycleStats) {
	t.Helper()
	if sWant.BookBuys != sHave.BookBuys {
		t.Errorf("wrong BookBuys. wanted %d, got %d", sWant.BookBuys, sHave.BookBuys)
	}
	// if sWant.BookBuys5 != sHave.BookBuys5 {
	// 	t.Errorf("wrong BookBuys5. wanted %d, got %d", sWant.BookBuys5, sHave.BookBuys5)
	// }
	// if sWant.BookBuys25 != sHave.BookBuys25 {
	// 	t.Errorf("wrong BookBuys25. wanted %d, got %d", sWant.BookBuys25, sHave.BookBuys25)
	// }
	if sWant.BookSells != sHave.BookSells {
		t.Errorf("wrong BookSells. wanted %d, got %d", sWant.BookSells, sHave.BookSells)
	}
	// if sWant.BookSells5 != sHave.BookSells5 {
	// 	t.Errorf("wrong BookSells5. wanted %d, got %d", sWant.BookSells5, sHave.BookSells5)
	// }
	// if sWant.BookSells25 != sHave.BookSells25 {
	// 	t.Errorf("wrong BookSells25. wanted %d, got %d", sWant.BookSells25, sHave.BookSells25)
	// }
	if sWant.MatchVolume != sHave.MatchVolume {
		t.Errorf("wrong MatchVolume. wanted %d, got %d", sWant.MatchVolume, sHave.MatchVolume)
	}
	if sWant.HighRate != sHave.HighRate {
		t.Errorf("wrong HighRate. wanted %d, got %d", sWant.HighRate, sHave.HighRate)
	}
	if sWant.LowRate != sHave.LowRate {
		t.Errorf("wrong LowRate. wanted %d, got %d", sWant.LowRate, sHave.LowRate)
	}
	if sWant.StartRate != sHave.StartRate {
		t.Errorf("wrong StartRate. wanted %d, got %d", sWant.StartRate, sHave.StartRate)
	}
	if sWant.EndRate != sHave.EndRate {
		t.Errorf("wrong EndRate. wanted %d, got %d", sWant.EndRate, sHave.EndRate)
	}
}
