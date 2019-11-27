// Package integ_test is a black-box integration test package.
// This file performs book-matcher integration tests.
package integ_test

import (
	"os"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/book"
	"decred.org/dcrdex/server/matcher"
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
	logger := slog.NewBackend(os.Stdout).Logger("MATCHTEST - book")
	logger.SetLevel(slog.LevelDebug)
	book.UseLogger(logger)

	logger = slog.NewBackend(os.Stdout).Logger("MATCHTEST - matcher")
	logger.SetLevel(slog.LevelDebug)
	matcher.UseLogger(logger)

	logger = slog.NewBackend(os.Stdout).Logger("MATCHTEST - order")
	logger.SetLevel(slog.LevelDebug)
	order.UseLogger(logger)
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
			UTXOs:    []order.Outpoint{},
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
		UTXOs:    []order.Outpoint{},
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
		UTXOs:    []order.Outpoint{},
		Sell:     false,
		Quantity: quantityQuoteAsset,
		Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
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

func newBook(t *testing.T) *book.Book {
	resetMakers()

	b := book.New(LotSize)

	for _, o := range bookBuyOrders {
		if ok := b.Insert(o); !ok {
			t.Fatalf("Failed to insert buy order %v", o)
		}
	}
	for _, o := range bookSellOrders {
		if ok := b.Insert(o); !ok {
			t.Fatalf("Failed to insert sell order %v", o)
		}
	}
	return b
}

func resetMakers() {
	for _, o := range bookBuyOrders {
		o.Filled = 0
	}
	for _, o := range bookSellOrders {
		o.Filled = 0
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

func TestMatchWithBook_limitsOnly(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := matcher.New()

	badLotsizeOrder := newLimitOrder(false, 05000000, 1, order.ImmediateTiF, 0)
	badLotsizeOrder.Quantity /= 2

	// takers is heterogenous w.r.t. type
	takers := []order.Order{
		newLimitOrder(false, 4550000, 1, order.ImmediateTiF, 0), // buy, 1 lot, immediate, equal rate
		newLimitOrder(false, 4550000, 2, order.StandingTiF, 0),  // buy, 2 lot, standing, equal rate, partial taker insert to book
		newLimitOrder(false, 4550000, 2, order.ImmediateTiF, 0), // buy, 2 lot, immediate, equal rate, partial taker unfilled
		newLimitOrder(false, 4100000, 1, order.ImmediateTiF, 0), // buy, 1 lot, immediate, unfilled fail
		newLimitOrder(true, 4540000, 1, order.ImmediateTiF, 5),  // sell, 1 lot, immediate
		newLimitOrder(true, 4300000, 4, order.ImmediateTiF, 0),  // sell, 4 lot, immediate, partial maker
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
	nBuy := len(bookBuyOrders)

	type args struct {
		book  *book.Book
		queue []order.Order
	}
	tests := []struct {
		name            string
		args            args
		doesMatch       bool
		wantMatches     []*order.MatchSet
		wantNumPassed   int
		wantNumFailed   int
		wantNumPartial  int
		wantNumBooked   int
		wantNumUnbooked int
	}{
		{
			name: "limit buy immediate rate match",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[0]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[0], []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 1,
		},
		{
			name: "limit buy standing partial taker inserted to book",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[1]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[1], []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  1,
			wantNumBooked:   1,
			wantNumUnbooked: 1,
		},
		{
			name: "limit buy immediate partial taker unfilled",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[2]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[2], []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  1,
			wantNumBooked:   0,
			wantNumUnbooked: 1,
		},
		{
			name: "limit buy immediate unfilled fail",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[3]},
			},
			doesMatch:       false,
			wantMatches:     nil,
			wantNumPassed:   0,
			wantNumFailed:   1,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 0,
		},
		{
			name: "bad lot size order",
			args: args{
				book:  newBook(t),
				queue: []order.Order{badLotsizeOrder},
			},
			doesMatch:       false,
			wantMatches:     nil,
			wantNumPassed:   0,
			wantNumFailed:   1,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 0,
		},
		{
			name: "limit buy standing partial taker inserted to book, then filled by down-queue sell",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[1], takers[4]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[1], []*order.LimitOrder{bookSellOrders[nSell-1]}),
				{ // the maker is reduced by matching first item in the queue
					Taker:   takers[4],
					Makers:  []*order.LimitOrder{takers[1].(*order.LimitOrder)},
					Amounts: []uint64{1 * LotSize}, // 2 - 1
					Rates:   []uint64{4550000},
					Total:   1 * LotSize,
				},
			},
			wantNumPassed:   2,
			wantNumFailed:   0,
			wantNumPartial:  1,
			wantNumBooked:   1,
			wantNumUnbooked: 2,
		},
		{
			name: "limit sell immediate rate overlap",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[5]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[5], []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2], bookBuyOrders[nBuy-3]}, 1*LotSize),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetTakers()
			resetMakers()

			matches, passed, failed, partial, booked, unbooked := me.Match(tt.args.book, tt.args.queue)
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
			if len(booked) != tt.wantNumBooked {
				t.Errorf("number booked %d, expected %d", len(booked), tt.wantNumBooked)
			}
			if len(unbooked) != tt.wantNumUnbooked {
				t.Errorf("number unbooked %d, expected %d", len(unbooked), tt.wantNumUnbooked)
			}
		})
	}
}

func orderInSlice(o order.Order, s []order.Order) int {
	for i := range s {
		if s[i].ID() == o.ID() {
			return i
		}
	}
	return -1
}

func TestMatchWithBook_limitsOnly_multipleQueued(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := matcher.New()

	// epochQueue is heterogenous w.r.t. type
	// var nonce int64
	epochQueue := []order.Order{
		// buys
		newLimitOrder(false, 4550000, 1, order.ImmediateTiF, 0), // 0: buy, 1 lot, immediate
		newLimitOrder(false, 4550000, 2, order.StandingTiF, 0),  // 1: buy, 2 lot, standing
		newLimitOrder(false, 4550000, 2, order.ImmediateTiF, 0), // 2: buy, 2 lot, immediate
		newLimitOrder(false, 4100000, 1, order.ImmediateTiF, 0), // 3: buy, 1 lot, immediate
		// sells
		newLimitOrder(true, 4540000, 1, order.ImmediateTiF, 0),              // 4: sell, 1 lot, immediate
		newLimitOrder(true, 4300000, 4, order.ImmediateTiF, 0),              // 5: sell, 4 lot, immediate
		newLimitOrder(true, 4720000, 40, order.StandingTiF, 4726 /*nonce*/), // 6: sell, 40 lot, standing, unfilled insert
	}
	epochQueueInit := make([]order.Order, len(epochQueue))
	copy(epochQueueInit, epochQueue)

	/* brute force a nonce to make changing the test less horrible
	// t.Log(epochQueue)
	// matcher.ShuffleQueue(epochQueue)

	// Apply the shuffling to determine matching order that will be used.
	wantOrder := []int{1, 6, 0, 3, 4, 5, 2}
	var wantQueue []order.Order
	for _, i := range wantOrder {
		wantQueue = append(wantQueue, epochQueueInit[i])
	}

	queuesEqual := func(q1, q2 []order.Order) bool {
		if len(q1) != len(q2) {
			return false
		}
		for i := range q1 {
			if q1[i].(*order.LimitOrder) != q2[i].(*order.LimitOrder) {
				return false
			}
			// if q1[i].ID() != q2[i].ID() {
			// 	return false
			// }
		}
		return true
	}

	lo6 := epochQueueInit[6].(*order.LimitOrder)
	for !queuesEqual(wantQueue, epochQueue) {
		lo6.ClientTime = lo6.ClientTime.Add(time.Second)
		lo6.ServerTime = lo6.ServerTime.Add(time.Second)
		lo6.ClearID()
		matcher.ShuffleQueue(epochQueue)
		nonce++
	}
	t.Logf("nonce: %d", nonce)
	t.Log(epochQueue)
	// NOTE: func (p *Prefix) ClearID() { p.id = nil }
	*/

	// -> Shuffles to [1, 6, 0, 3, 4, 5, 2]
	// 1 -> partial match, inserted into book (passed, partial inserted)
	// 6 -> inserted into book (partial, inserted)
	// 0 -> is unfilled (failed)
	// 3 -> is unfilled (failed)
	// 4 -> fills against order 1, which was just inserted (passed)
	// 5 -> is filled (passed)
	// 2 -> is unfilled (failed)
	// matches: [1, 4, 5], passed: [1, 4], failed: [0, 3, 2]
	// partial: [1, 6], inserted: [1, 6]

	// order book from bookBuyOrders and bookSellOrders
	b := newBook(t)

	resetQueue := func() {
		for _, o := range epochQueue {
			switch ot := o.(type) {
			case *order.MarketOrder:
				ot.Filled = 0
			case *order.LimitOrder:
				ot.Filled = 0
			}
		}
	}

	// nSell := len(bookSellOrders)
	// nBuy := len(bookBuyOrders)

	// Reset Filled amounts of all pre-defined orders before each test.
	resetQueue()
	resetMakers()

	matches, passed, failed, partial, booked, unbooked := me.Match(b, epochQueue)
	//t.Log(matches, passed, failed, partial, booked, unbooked)

	// PASSED orders

	// epoch order 0 should be order 0 in passed slice
	expectedLoc := 0
	if loc := orderInSlice(epochQueueInit[1], passed); loc == -1 {
		t.Errorf("Order not in passed slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in passed slice: %d", loc)
	}

	// epoch order 5 should be order 1 in passed slice
	expectedLoc = 1
	if loc := orderInSlice(epochQueueInit[4], passed); loc == -1 {
		t.Errorf("Order not in passed slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in passed slice: %d", loc)
	}

	// FAILED orders

	// epoch order 3 should be order 0 in failed slice
	expectedLoc = 0
	if loc := orderInSlice(epochQueueInit[0], failed); loc == -1 {
		t.Errorf("Order not in failed slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in failed slice: %d", loc)
	}

	// epoch order 4 should be order 1 in failed slice
	expectedLoc = 1
	if loc := orderInSlice(epochQueueInit[3], failed); loc == -1 {
		t.Errorf("Order not in failed slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in failed slice: %d", loc)
	}

	// epoch order 2 should be order 2 in failed slice
	expectedLoc = 2
	if loc := orderInSlice(epochQueueInit[2], failed); loc == -1 {
		t.Errorf("Order not in failed slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in failed slice: %d", loc)
	}

	// PARTIAL fills

	// epoch order 1 should be order 0 in partial slice
	expectedLoc = 0
	if loc := orderInSlice(epochQueueInit[1], partial); loc == -1 {
		t.Errorf("Order not in partial slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in partial slice: %d", loc)
	}

	// epoch order 6 should be order 1 in partial slice
	expectedLoc = 1
	if loc := orderInSlice(epochQueueInit[6], partial); loc == -1 {
		t.Errorf("Order not in partial slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in partial slice: %d", loc)
	}

	// BOOKED orders

	// epoch order 1 should be order 0 in booked slice
	expectedLoc = 0
	if loc := orderInSlice(epochQueueInit[1], booked); loc == -1 {
		t.Errorf("Order not in booked slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in booked slice: %d", loc)
	}

	// epoch order 6 should be order 1 in booked slice
	expectedLoc = 1
	if loc := orderInSlice(epochQueueInit[6], booked); loc == -1 {
		t.Errorf("Order not in booked slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in booked slice: %d", loc)
	}

	// epoch order 1 should be order 1 in unbooked slice
	expectedLoc = 1
	if loc := orderInSlice(epochQueueInit[1], unbooked); loc == -1 {
		t.Errorf("Order not in unbooked slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in unbooked slice: %d", loc)
	}

	// epoch order 5 (sell, 4 lots, immediate @ 4300000) is match 1, matched
	// with 3 orders, the first of which of which is epoch order 1 (buy, 2 lots,
	// standing @ 4550000) that was booked as a standing order.
	if matches[1].Taker.ID() != epochQueueInit[4].ID() {
		t.Errorf("Taker order ID expected %v, got %v",
			epochQueueInit[5].UID(), matches[1].Taker.UID())
	}
	if matches[1].Makers[0].ID() != epochQueueInit[1].ID() {
		t.Errorf("First match was expected to be %v, got %v",
			epochQueueInit[1].ID(), matches[1].Makers[0].ID())
	}
}

func newCancelOrder(targetOrderID order.OrderID, serverTime time.Time) *order.CancelOrder {
	return &order.CancelOrder{
		Prefix:        order.Prefix{ServerTime: serverTime},
		TargetOrderID: targetOrderID,
	}
}

func TestMatch_cancelOnly(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := matcher.New()

	fakeOrder := newLimitOrder(false, 4550000, 1, order.ImmediateTiF, 0)
	fakeOrder.ServerTime = time.Unix(1566497654, 0)

	// takers is heterogenous w.r.t. type
	takers := []order.Order{
		newCancelOrder(bookBuyOrders[3].ID(), fakeOrder.ServerTime.Add(time.Second)),
		newCancelOrder(fakeOrder.ID(), fakeOrder.ServerTime.Add(time.Second)),
	}

	//nSell := len(bookSellOrders)
	//nBuy := len(bookBuyOrders)

	type args struct {
		book  *book.Book
		queue []order.Order
	}
	tests := []struct {
		name            string
		args            args
		doesMatch       bool
		wantMatches     []*order.MatchSet
		wantNumPassed   int
		wantNumFailed   int
		wantNumPartial  int
		wantNumBooked   int
		wantNumUnbooked int
	}{
		{
			name: "cancel standing ok",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[0]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				{
					Taker:   takers[0],
					Makers:  []*order.LimitOrder{bookBuyOrders[3]},
					Amounts: []uint64{bookBuyOrders[3].Remaining()},
					Rates:   []uint64{bookBuyOrders[3].Rate},
				},
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 1,
		},
		{
			name: "cancel non-existent standing",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[1]},
			},
			doesMatch:       false,
			wantMatches:     nil,
			wantNumPassed:   0,
			wantNumFailed:   1,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 0,
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

			matches, passed, failed, partial, booked, unbooked := me.Match(tt.args.book, tt.args.queue)
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
			if len(booked) != tt.wantNumBooked {
				t.Errorf("number booked %d, expected %d", len(booked), tt.wantNumBooked)
			}
			if len(unbooked) != tt.wantNumUnbooked {
				t.Errorf("number unbooked %d, expected %d", len(unbooked), tt.wantNumUnbooked)
			}

			numBuys1 := tt.args.book.BuyCount()
			if numBuys0-len(passed) != numBuys1 {
				t.Errorf("Buy side order book size %d, expected %d", numBuys1, numBuys0-len(passed))
			}
		})
	}
}

func TestMatch_marketSellsOnly(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := matcher.New()

	badLotsizeOrder := newMarketSellOrder(1, 0)
	badLotsizeOrder.Quantity /= 2

	// takers is heterogenous w.r.t. type
	takers := []order.Order{
		newMarketSellOrder(1, 0),  // sell, 1 lot
		newMarketSellOrder(3, 0),  // sell, 5 lot
		newMarketSellOrder(5, 0),  // sell, 6 lot, partial maker fill
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
		book  *book.Book
		queue []order.Order
	}
	tests := []struct {
		name            string
		args            args
		doesMatch       bool
		wantMatches     []*order.MatchSet
		wantNumPassed   int
		wantNumFailed   int
		wantNumPartial  int
		wantNumBooked   int
		wantNumUnbooked int
	}{
		{
			name: "market sell, 1 maker match",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[0]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[0], []*order.LimitOrder{bookBuyOrders[nBuy-1]}),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 1,
		},
		{
			name: "market sell, 2 maker match",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[1]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[1], []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2]}),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 2,
		},
		{
			name: "market sell, 2 maker match, partial maker fill",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[2]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[2], []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2], bookBuyOrders[nBuy-3]}, 2*LotSize),
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 2,
		},
		{
			name: "market sell bad lot size",
			args: args{
				book:  newBook(t),
				queue: []order.Order{badLotsizeOrder},
			},
			doesMatch:       false,
			wantMatches:     nil,
			wantNumPassed:   0,
			wantNumFailed:   1,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetTakers()
			resetMakers()

			//fmt.Printf("%v\n", takers)

			matches, passed, failed, partial, booked, unbooked := me.Match(tt.args.book, tt.args.queue)
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
			if len(booked) != tt.wantNumBooked {
				t.Errorf("number booked %d, expected %d", len(booked), tt.wantNumBooked)
			}
			if len(unbooked) != tt.wantNumUnbooked {
				t.Errorf("number unbooked %d, expected %d", len(unbooked), tt.wantNumUnbooked)
			}
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

		amt += matcher.BaseToQuote(sellOrder.Rate, orderLots*LotSize)
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
	me := matcher.New()

	nSell := len(bookSellOrders)
	//nBuy := len(bookBuyOrders)

	// takers is heterogenous w.r.t. type
	takers := []order.Order{
		newMarketBuyOrder(quoteAmt(1), 0),  // buy, 1 lot
		newMarketBuyOrder(quoteAmt(2), 0),  // buy, 2 lot
		newMarketBuyOrder(quoteAmt(3), 0),  // buy, 3 lot
		newMarketBuyOrder(quoteAmt(99), 0), // buy, up to 99 lots, computed exactly for the book
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
		book  *book.Book
		queue []order.Order
	}
	tests := []struct {
		name            string
		args            args
		doesMatch       bool
		wantMatches     []*order.MatchSet
		remaining       []uint64
		wantNumPassed   int
		wantNumFailed   int
		wantNumPartial  int
		wantNumBooked   int
		wantNumUnbooked int
	}{
		{
			name: "market buy, 1 maker match",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[0]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[0], []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			remaining:       []uint64{quoteAmt(1) - marketBuyQuoteAmt(1)},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 1,
		},
		{
			name: "market buy, 2 maker match",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[1]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[1], []*order.LimitOrder{bookSellOrders[nSell-1], bookSellOrders[nSell-2]}, 1*LotSize),
			},
			remaining:       []uint64{0},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 1,
		},
		{
			name: "market buy, 3 maker match",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[2]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[2], []*order.LimitOrder{bookSellOrders[nSell-1], bookSellOrders[nSell-2]}),
			},
			remaining:       []uint64{0},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 2,
		},
		{
			name: "market buy, 99 maker match",
			args: args{
				book:  newBook(t),
				queue: []order.Order{takers[3]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[3], bookSellOrdersReverse),
			},
			remaining:       []uint64{0},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 11,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetTakers()
			resetMakers()

			matches, passed, failed, partial, booked, unbooked := me.Match(tt.args.book, tt.args.queue)
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
				if matches[i].Taker.Remaining() != tt.remaining[i] {
					t.Errorf("Incorrect taker order amount remaining. Expected %d, got %d",
						tt.remaining[i], matches[i].Taker.Remaining())
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
			if len(booked) != tt.wantNumBooked {
				t.Errorf("number booked %d, expected %d", len(booked), tt.wantNumBooked)
			}
			if len(unbooked) != tt.wantNumUnbooked {
				t.Errorf("number unbooked %d, expected %d", len(unbooked), tt.wantNumUnbooked)
			}
		})
	}
}

func TestMatchWithBook_everything_multipleQueued(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := matcher.New()

	nSell := len(bookSellOrders)
	nBuy := len(bookBuyOrders)
	cancelTime := time.Unix(1566497656, 0)

	// epochQueue is heterogenous w.r.t. type
	epochQueue := []order.Order{
		// buys
		newLimitOrder(false, 4550000, 1, order.ImmediateTiF, 0), // 0: buy, 1 lot, immediate
		newLimitOrder(false, 4550000, 2, order.StandingTiF, 0),  // 1: buy, 2 lot, standing
		newLimitOrder(false, 4550000, 2, order.ImmediateTiF, 0), // 2: buy, 2 lot, immediate
		newLimitOrder(false, 4100000, 1, order.ImmediateTiF, 0), // 3: buy, 1 lot, immediate
		// sells
		newLimitOrder(true, 4540000, 1, order.ImmediateTiF, 0), // 4: sell, 1 lot, immediate
		newLimitOrder(true, 4800000, 4, order.StandingTiF, 0),  // 5: sell, 4 lot, immediate
		newLimitOrder(true, 4300000, 4, order.ImmediateTiF, 0), // 6: sell, 4 lot, immediate
		newLimitOrder(true, 4800000, 40, order.StandingTiF, 0), // 7: sell, 40 lot, standing, unfilled insert
		// market
		newMarketSellOrder(2, 0),          // 8
		newMarketSellOrder(4, 0),          // 9
		newMarketBuyOrder(quoteAmt(1), 0), // 10
		newMarketBuyOrder(quoteAmt(2), 0), // 11
		// cancel
		newCancelOrder(bookSellOrders[6].ID(), cancelTime),       // 12
		newCancelOrder(bookBuyOrders[8].ID(), cancelTime),        // 13
		newCancelOrder(bookBuyOrders[nBuy-1].ID(), cancelTime),   // 14
		newCancelOrder(bookSellOrders[nSell-1].ID(), cancelTime), // 15
	}
	// cancel some the epoch queue orders too
	epochQueue = append(epochQueue, newCancelOrder(epochQueue[7].ID(), cancelTime)) // 16
	epochQueue = append(epochQueue, newCancelOrder(epochQueue[5].ID(), cancelTime)) // 17

	epochQueueInit := make([]order.Order, len(epochQueue))
	copy(epochQueueInit, epochQueue)

	// var shuf []int
	// matcher.ShuffleQueue(epochQueue)
	// for i := range epochQueue {
	// 	for j := range epochQueueInit {
	// 		if epochQueue[i].ID() == epochQueueInit[j].ID() {
	// 			shuf = append(shuf, j)
	// 			t.Logf("%d: %p", j, epochQueueInit[j])
	// 			continue
	// 		}
	// 	}
	// }
	// t.Logf("%#v", shuf)

	// Apply the shuffling to determine matching order that will be used.
	// matcher.ShuffleQueue(epochQueue)
	// for i := range epochQueue {
	// 	t.Logf("%d: %p, %p", i, epochQueueInit[i], epochQueue[i])
	// }
	// Shuffles to [7, 1, 12, 11, 2, 14, 3, 17, 15, 13, 10, 5, 0, 6, 9, 4, 16, 8]

	expectedNumMatches := 10
	expectedPassed := []int{1, 12, 11, 14, 13, 10, 6, 9, 16, 8}
	expectedFailed := []int{2, 3, 17, 15, 0, 4}
	expectedPartial := []int{7, 1, 5, 6}
	expectedBooked := []int{7, 1, 5} // all StandingTiF
	expectedUnbooked := []int{}

	// order book from bookBuyOrders and bookSellOrders
	b := newBook(t)

	resetQueue := func() {
		for _, o := range epochQueue {
			switch ot := o.(type) {
			case *order.MarketOrder:
				ot.Filled = 0
			case *order.LimitOrder:
				ot.Filled = 0
			}
		}
	}

	// Reset Filled amounts of all pre-defined orders before each test.
	resetQueue()
	resetMakers()

	matches, passed, failed, partial, booked, unbooked := me.Match(b, epochQueue)
	//t.Log("Matches:", matches)
	// s := "Passed: "
	// for _, o := range passed {
	// 	s += fmt.Sprintf("%p ", o)
	// }
	// t.Log(s)
	// s = "Failed: "
	// for _, o := range failed {
	// 	s += fmt.Sprintf("%p ", o)
	// }
	// t.Log(s)
	// s = "Partial: "
	// for _, o := range partial {
	// 	s += fmt.Sprintf("%p ", o)
	// }
	// t.Log(s)
	// s = "Booked: "
	// for _, o := range booked {
	// 	s += fmt.Sprintf("%p ", o)
	// }
	// t.Log(s)
	// s = "Unbooked: "
	// for _, o := range unbooked {
	// 	s += fmt.Sprintf("%p ", o)
	// }
	// t.Log(s)

	// for i := range matches {
	// 	t.Logf("Match %d: %p, [%p, ...]", i, matches[i].Taker, matches[i].Makers[0])
	// }

	// PASSED orders

	for i, qi := range expectedPassed {
		if oi := orderInSlice(epochQueueInit[qi], passed); oi != i {
			t.Errorf("Order not at expected location in passed slice. Got %d, expected %d",
				oi, i)
		}
	}

	for i, qi := range expectedFailed {
		if oi := orderInSlice(epochQueueInit[qi], failed); oi != i {
			t.Errorf("Order not at expected location in failed slice. Got %d, expected %d",
				oi, i)
		}
	}

	for i, qi := range expectedPartial {
		if oi := orderInSlice(epochQueueInit[qi], partial); oi != i {
			t.Errorf("Order not at expected location in partial slice. Got %d, expected %d",
				oi, i)
		}
	}

	for i, qi := range expectedBooked {
		if oi := orderInSlice(epochQueueInit[qi], booked); oi != i {
			t.Errorf("Order not at expected location in booked slice. Got %d, expected %d",
				oi, i)
		}
	}

	for i, qi := range expectedUnbooked {
		if oi := orderInSlice(epochQueueInit[qi], unbooked); oi != i {
			t.Errorf("Order not at expected location in unbooked slice. Got %d, expected %d",
				oi, i)
		}
	}

	if len(matches) != expectedNumMatches {
		t.Errorf("Incorrect number of matches. Got %d, expected %d", len(matches), expectedNumMatches)
	}

	// match 3 (epoch order 14) cancels a book order
	if matches[3].Taker.ID() != epochQueueInit[14].ID() {
		t.Errorf("Taker order ID expected %v, got %v",
			epochQueueInit[14].UID(), matches[3].Taker.UID())
	}
	if matches[8].Taker.ID() != epochQueueInit[16].ID() {
		t.Errorf("8th match take was expected to be %v, got %v",
			epochQueueInit[16], matches[8].Taker.ID())
	}
}
