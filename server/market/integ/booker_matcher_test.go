// Package integ_test is a black-box integration test package.
// This file performs book-matcher integration tests.
package integ_test

import (
	"math/rand"
	"reflect"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/account"
	"decred.org/dcrdex/server/book"
	"decred.org/dcrdex/server/matcher"
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

func startLogger() {
	logger := dex.StdOutLogger("MATCHTEST - book", dex.LevelTrace)
	book.UseLogger(logger)

	logger = dex.StdOutLogger("MATCHTEST - matcher", dex.LevelTrace)
	matcher.UseLogger(logger)
}

func randomPreimage() (pe order.Preimage) {
	rand.Read(pe[:])
	return
}

func newLimitOrder(sell bool, rate, quantityLots uint64, force order.TimeInForce, timeOffset int64) *order.LimitOrder {
	return newLimit(sell, rate, quantityLots, force, timeOffset).Order.(*order.LimitOrder)
}

func newLimit(sell bool, rate, quantityLots uint64, force order.TimeInForce, timeOffset int64) *matcher.OrderRevealed {
	addr := "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui"
	if sell {
		addr = "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm"
	}
	pi := randomPreimage()
	return &matcher.OrderRevealed{
		Order: &order.LimitOrder{
			P: order.Prefix{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  order.LimitOrderType,
				ClientTime: time.Unix(1566497653+timeOffset, 0),
				ServerTime: time.Unix(1566497656+timeOffset, 0),
				Commit:     pi.Commit(),
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
		Preimage: pi,
	}
}

func newMarketSellOrder(quantityLots uint64, timeOffset int64) *order.MarketOrder {
	return newMarketSell(quantityLots, timeOffset).Order.(*order.MarketOrder)
}

func newMarketSell(quantityLots uint64, timeOffset int64) *matcher.OrderRevealed {
	pi := randomPreimage()
	return &matcher.OrderRevealed{
		Order: &order.MarketOrder{
			P: order.Prefix{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  order.MarketOrderType,
				ClientTime: time.Unix(1566497653+timeOffset, 0),
				ServerTime: time.Unix(1566497656+timeOffset, 0),
				Commit:     pi.Commit(),
			},
			T: order.Trade{
				Coins:    []order.CoinID{},
				Sell:     true,
				Quantity: quantityLots * LotSize,
				Address:  "149RQGLaHf2gGiL4NXZdH7aA8nYEuLLrgm",
			},
		},
		Preimage: pi,
	}
}

func newMarketBuyOrder(quantityQuoteAsset uint64, timeOffset int64) *order.MarketOrder {
	return newMarketBuy(quantityQuoteAsset, timeOffset).Order.(*order.MarketOrder)
}

func newMarketBuy(quantityQuoteAsset uint64, timeOffset int64) *matcher.OrderRevealed {
	pi := randomPreimage()
	return &matcher.OrderRevealed{
		Order: &order.MarketOrder{
			P: order.Prefix{
				AccountID:  acct0,
				BaseAsset:  AssetDCR,
				QuoteAsset: AssetBTC,
				OrderType:  order.MarketOrderType,
				ClientTime: time.Unix(1566497653+timeOffset, 0),
				ServerTime: time.Unix(1566497656+timeOffset, 0),
				Commit:     pi.Commit(),
			},
			T: order.Trade{
				Coins:    []order.CoinID{},
				Sell:     false,
				Quantity: quantityQuoteAsset,
				Address:  "DcqXswjTPnUcd4FRCkX4vRJxmVtfgGVa5ui",
			},
		},
		Preimage: pi,
	}
}

func newCancel(targetOrderID order.OrderID, serverTime time.Time) *matcher.OrderRevealed {
	pi := randomPreimage()
	return &matcher.OrderRevealed{
		Order: &order.CancelOrder{
			P: order.Prefix{
				ServerTime: serverTime,
				Commit:     pi.Commit(),
			},
			TargetOrderID: targetOrderID,
		},
		Preimage: pi,
	}
}

func newCancelOrder(targetOrderID order.OrderID, serverTime time.Time) *order.CancelOrder {
	return newCancel(targetOrderID, serverTime).Order.(*order.CancelOrder)
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
	initialMidGap = (4550000 + 4500000) / 2
)

func newBook(t *testing.T) *book.Book {
	resetMakers()

	b := book.New(LotSize, 0)

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

func TestMatchWithBook_limitsOnly(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := matcher.New()

	rand.Seed(0)

	badLotsizeOrder := newLimit(false, 05000000, 1, order.ImmediateTiF, 0)
	badLotsizeOrder.Order.(*order.LimitOrder).Quantity /= 2

	// takers is heterogenous w.r.t. type
	takers := []*matcher.OrderRevealed{
		newLimit(false, 4550000, 1, order.ImmediateTiF, 0), // buy, 1 lot, immediate, equal rate
		newLimit(false, 4550000, 2, order.StandingTiF, 0),  // buy, 2 lot, standing, equal rate, partial taker insert to book
		newLimit(false, 4550000, 2, order.ImmediateTiF, 0), // buy, 2 lot, immediate, equal rate, partial taker unfilled
		newLimit(false, 4100000, 1, order.ImmediateTiF, 0), // buy, 1 lot, immediate, unfilled fail
		newLimit(true, 4540000, 1, order.ImmediateTiF, 0),  // sell, 1 lot, immediate
		newLimit(true, 4300000, 4, order.ImmediateTiF, 0),  // sell, 4 lot, immediate, partial maker
	}

	// tweak taker[4] commitment to get desired order.
	takers[4].Preimage[0] += 0 // brute forced, could have required multiple bytes changed
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
	nBuy := len(bookBuyOrders)

	type args struct {
		book  *book.Book
		queue []*matcher.OrderRevealed
	}
	tests := []struct {
		name             string
		args             args
		doesMatch        bool
		wantMatches      []*order.MatchSet
		wantNumPassed    int
		wantNumFailed    int
		wantDoneOK       int
		wantNumPartial   int
		wantNumBooked    int
		wantNumUnbooked  int
		wantNumNomatched int
	}{
		{
			name: "limit buy immediate rate match",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[0]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[0].Order, []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			wantNumPassed:    1,
			wantNumFailed:    0,
			wantDoneOK:       1,
			wantNumPartial:   0,
			wantNumBooked:    0,
			wantNumUnbooked:  1,
			wantNumNomatched: 0,
		},
		{
			name: "limit buy standing partial taker inserted to book",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[1]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[1].Order, []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			wantNumPassed:    1,
			wantNumFailed:    0,
			wantDoneOK:       0,
			wantNumPartial:   1,
			wantNumBooked:    1,
			wantNumUnbooked:  1,
			wantNumNomatched: 0,
		},
		{
			name: "limit buy immediate partial taker unfilled",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[2]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[2].Order, []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			wantNumPassed:    1,
			wantNumFailed:    0,
			wantDoneOK:       1,
			wantNumPartial:   1,
			wantNumBooked:    0,
			wantNumUnbooked:  1,
			wantNumNomatched: 0,
		},
		{
			name: "limit buy immediate unfilled fail",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[3]},
			},
			doesMatch:        false,
			wantMatches:      nil,
			wantNumPassed:    0,
			wantNumFailed:    1,
			wantDoneOK:       0,
			wantNumPartial:   0,
			wantNumBooked:    0,
			wantNumUnbooked:  0,
			wantNumNomatched: 1,
		},
		{
			name: "bad lot size order",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{badLotsizeOrder},
			},
			doesMatch:        false,
			wantMatches:      nil,
			wantNumPassed:    0,
			wantNumFailed:    1,
			wantDoneOK:       0,
			wantNumPartial:   0,
			wantNumBooked:    0,
			wantNumUnbooked:  0,
			wantNumNomatched: 0,
		},
		{
			name: "limit buy standing partial taker inserted to book, then filled by down-queue sell",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[1], takers[4]},
			},
			doesMatch: true,
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
			wantNumPassed:    2,
			wantNumFailed:    0,
			wantDoneOK:       1,
			wantNumPartial:   1,
			wantNumBooked:    1,
			wantNumUnbooked:  2,
			wantNumNomatched: 0,
		},
		{
			name: "limit sell immediate rate overlap",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[5]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(
					takers[5].Order,
					[]*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2], bookBuyOrders[nBuy-3]},
					1*LotSize),
			},
			wantNumPassed:    1,
			wantNumFailed:    0,
			wantDoneOK:       1,
			wantNumPartial:   0,
			wantNumBooked:    0,
			wantNumUnbooked:  2,
			wantNumNomatched: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetTakers()
			resetMakers()

			// Ignore the seed since it is tested in the matcher unit tests.
			_, matches, passed, failed, doneOK, partial, booked, nomatched, unbooked, _, _ := me.Match(tt.args.book, tt.args.queue)
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
			if len(doneOK) != tt.wantDoneOK {
				t.Errorf("number doneOK %d, expected %d", len(doneOK), tt.wantDoneOK)
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

			if len(nomatched) != tt.wantNumNomatched {
				t.Errorf("number nomatched %d, expected %d", len(nomatched), tt.wantNumNomatched)
			}
		})
	}
}

func orderInSlice(o *matcher.OrderRevealed, s []*matcher.OrderRevealed) int {
	for i := range s {
		if s[i].Order.ID() == o.Order.ID() {
			return i
		}
	}
	return -1
}

func orderInLimitSlice(o order.Order, s []*order.LimitOrder) int {
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

	rand.Seed(0)

	// epochQueue is heterogenous w.r.t. type
	epochQueue := []*matcher.OrderRevealed{
		// buys
		newLimit(false, 4550000, 1, order.ImmediateTiF, 0), // 0: buy, 1 lot, immediate
		newLimit(false, 4550000, 2, order.StandingTiF, 0),  // 1: buy, 2 lot, standing
		newLimit(false, 4550000, 2, order.ImmediateTiF, 0), // 2: buy, 2 lot, immediate
		newLimit(false, 4100000, 1, order.ImmediateTiF, 0), // 3: buy, 1 lot, immediate
		// sells
		newLimit(true, 4540000, 1, order.ImmediateTiF, 0), // 4: sell, 1 lot, immediate
		newLimit(true, 4300000, 4, order.ImmediateTiF, 0), // 5: sell, 4 lot, immediate
		newLimit(true, 4720000, 40, order.StandingTiF, 0), // 6: sell, 40 lot, standing, unfilled insert
	}
	epochQueue[0].Preimage = order.Preimage{
		0xb1, 0xcb, 0x0a, 0xc8, 0xbf, 0x2b, 0xa9, 0xa7,
		0x05, 0xf9, 0x6d, 0x6b, 0x68, 0x21, 0x28, 0x87,
		0x13, 0x26, 0x23, 0x80, 0xfb, 0xe6, 0xb9, 0x0f,
		0x74, 0x39, 0xc9, 0xf1, 0xcd, 0x6e, 0x02, 0xa8}
	epochQueue[0].Order.(*order.LimitOrder).Commit = epochQueue[0].Preimage.Commit()
	epochQueueInit := make([]*matcher.OrderRevealed, len(epochQueue))
	copy(epochQueueInit, epochQueue)

	/* //brute force a commitment to make changing the test less horrible
	t.Log(epochQueue)
	matcher.ShuffleQueue(epochQueue)

	// Apply the shuffling to determine matching order that will be used.
	wantOrder := []int{1, 6, 0, 3, 4, 5, 2}
	var wantQueue []*matcher.OrderRevealed
	for _, i := range wantOrder {
		wantQueue = append(wantQueue, epochQueueInit[i])
	}

	queuesEqual := func(q1, q2 []*matcher.OrderRevealed) bool {
		if len(q1) != len(q2) {
			return false
		}
		for i := range q1 {
			if q1[i].Order.(*order.LimitOrder) != q2[i].Order.(*order.LimitOrder) {
				return false
			}
		}
		return true
	}

	orderX := epochQueueInit[0]
	loX := orderX.Order.(*order.LimitOrder)
	var pi order.Preimage
	var i int
	for !queuesEqual(wantQueue, epochQueue) {
		pi = randomPreimage()
		orderX.Preimage = pi
		loX.Commit = pi.Commit()
		loX.SetTime(loX.ServerTime) // force recomputation of order ID
		matcher.ShuffleQueue(epochQueue)
		i++
	}
	t.Logf("preimage: %#v, commit: %#v", pi, loX.Commit)
	t.Log(i, epochQueue)
	*/

	// -> Shuffles to [1, 6, 0, 3, 4, 5, 2]
	// 1 -> partial match, inserted into book (passed, partial inserted), 1 lot @ 4550000, buyvol + 1, sellvol - 1 = 0
	// 6 -> unmatched, inserted into book (inserted, nomatched), sellvol + 40
	// 0 -> is unfilled (failed, nomatched)
	// 3 -> is unfilled (failed, nomatched)
	// 4 -> fills against order 1, which was just inserted (passed), 1 lot @ 4550000, buyvol - 1
	// 5 -> is filled (passed) 1 @ 4500000, 3 @ 4300000, buyvol - 4
	// 2 -> is unfilled (failed, nomatched)
	// matches: [1, 4, 5], passed: [1, 4], failed: [0, 3, 2]
	// partial: [1], inserted: [1, 6], nomatched: [6, 0, 3, 2]

	// best remaining sell: 4600000, buy: 4300000

	// order book from bookBuyOrders and bookSellOrders
	b := newBook(t)

	resetQueue := func() {
		for _, o := range epochQueue {
			switch ot := o.Order.(type) {
			case *order.MarketOrder:
				ot.FillAmt = 0
			case *order.LimitOrder:
				ot.FillAmt = 0
			}
		}
	}

	// nSell := len(bookSellOrders)
	// nBuy := len(bookBuyOrders)

	// Reset Filled amounts of all pre-defined orders before each test.
	resetQueue()
	resetMakers()

	// Ignore the seed since it is tested in the matcher unit tests.
	_, matches, passed, failed, doneOK, partial, booked, nomatched, unbooked, _, stats := me.Match(b, epochQueue)
	//t.Log(matches, passed, failed, doneOK, partial, booked, unbooked)

	lastMatch := matches[len(matches)-1]

	compareMatchStats(t, &matcher.MatchCycleStats{
		BookBuys:    (bookBuyLots - 4) * LotSize,
		BookSells:   (bookSellLots + 39) * LotSize,
		MatchVolume: 6 * LotSize,
		HighRate:    4550000,
		LowRate:     4300000,
		StartRate:   matches[0].Makers[0].Rate,
		EndRate:     lastMatch.Makers[len(lastMatch.Makers)-1].Rate,
	}, stats)

	// PASSED orders

	// epoch order 0 should be order 0 in passed slice
	expectedLoc := 0
	if loc := orderInSlice(epochQueueInit[1], passed); loc == -1 {
		t.Errorf("Order not in passed slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in passed slice: %d", loc)
	}

	// epoch order 5 should be order 2 in passed slice
	expectedLoc = 2
	if loc := orderInSlice(epochQueueInit[4], passed); loc == -1 {
		t.Errorf("Order not in passed slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in passed slice: %d", loc)
	}

	//t.Log(doneOK)

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

	// Done OK
	expectedLoc = 1
	if loc := orderInSlice(epochQueueInit[5], doneOK); loc == -1 {
		t.Errorf("Order not in doneOK slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in doneOK slice: %d", loc)
	}

	// PARTIAL fills

	// epoch order 1 should be order 0 in partial slice
	expectedLoc = 0
	if loc := orderInSlice(epochQueueInit[1], partial); loc == -1 {
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
	if loc := orderInLimitSlice(epochQueueInit[1].Order, unbooked); loc == -1 {
		t.Errorf("Order not in unbooked slice.")
	} else if loc != expectedLoc {
		t.Errorf("Order not at expected location in unbooked slice: %d", loc)
	}

	// NOMATCHED orders

	// We don't know the exact order, since there is an intermediate map used
	// for tracking.
	if len(nomatched) != 4 {
		t.Errorf("Wrong number of nomatched orders. Wanted 4, got %d", len(nomatched))
	} else {
		for _, i := range []int{6, 0, 3, 2} {
			if orderInSlice(epochQueueInit[i], nomatched) == -1 {
				t.Errorf("Epoch queue order %d not in nomatched slice", i)
			}
		}
	}

	// epoch order 5 (sell, 4 lots, immediate @ 4300000) is match 1, matched
	// with 3 orders, the first of which of which is epoch order 1 (buy, 2 lots,
	// standing @ 4550000) that was booked as a standing order.
	if matches[1].Taker.ID() != epochQueueInit[4].Order.ID() {
		t.Errorf("Taker order ID expected %v, got %v",
			epochQueueInit[5].Order.UID(), matches[1].Taker.UID())
	}
	if matches[1].Makers[0].ID() != epochQueueInit[1].Order.ID() {
		t.Errorf("First match was expected to be %v, got %v",
			epochQueueInit[1].Order.ID(), matches[1].Makers[0].ID())
	}
}

func TestMatch_cancelOnly(t *testing.T) {
	// Setup the match package's logger.
	startLogger()

	// New matching engine.
	me := matcher.New()

	rand.Seed(0)

	fakeOrder := newLimitOrder(false, 4550000, 1, order.ImmediateTiF, 0)
	fakeOrder.ServerTime = time.Unix(1566497654, 0)

	// takers is heterogenous w.r.t. type
	takers := []*matcher.OrderRevealed{
		newCancel(bookBuyOrders[3].ID(), fakeOrder.ServerTime.Add(time.Second)),
		newCancel(fakeOrder.ID(), fakeOrder.ServerTime.Add(time.Second)),
	}

	//nSell := len(bookSellOrders)
	//nBuy := len(bookBuyOrders)

	type args struct {
		book  *book.Book
		queue []*matcher.OrderRevealed
	}
	tests := []struct {
		name            string
		args            args
		doesMatch       bool
		wantMatches     []*order.MatchSet
		wantNumPassed   int
		wantNumFailed   int
		wantDoneOK      int
		wantNumPartial  int
		wantNumBooked   int
		wantNumUnbooked int
	}{
		{
			name: "cancel standing ok",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[0]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				{
					Taker:   takers[0].Order,
					Makers:  []*order.LimitOrder{bookBuyOrders[3]},
					Amounts: []uint64{bookBuyOrders[3].Remaining()},
					Rates:   []uint64{bookBuyOrders[3].Rate},
				},
			},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantDoneOK:      1,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 1,
		},
		{
			name: "cancel non-existent standing",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[1]},
			},
			doesMatch:       false,
			wantMatches:     nil,
			wantNumPassed:   0,
			wantNumFailed:   1,
			wantDoneOK:      0,
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

			// Ignore the seed since it is tested in the matcher unit tests.
			_, matches, passed, failed, doneOK, partial, booked, _, unbooked, _, _ := me.Match(tt.args.book, tt.args.queue)
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
			if len(doneOK) != tt.wantDoneOK {
				t.Errorf("number doneOK %d, expected %d", len(doneOK), tt.wantDoneOK)
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

	rand.Seed(0)

	badLotsizeOrder := newMarketSell(1, 0)
	badLotsizeOrder.Order.(*order.MarketOrder).Quantity /= 2

	// takers is heterogenous w.r.t. type
	takers := []*matcher.OrderRevealed{
		newMarketSell(1, 0),  // sell, 1 lot
		newMarketSell(3, 0),  // sell, 3 lot
		newMarketSell(5, 0),  // sell, 5 lot, partial maker fill
		newMarketSell(99, 0), // sell, 99 lot, partial taker fill
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

	//nSell := len(bookSellOrders)
	nBuy := len(bookBuyOrders)

	type args struct {
		book  *book.Book
		queue []*matcher.OrderRevealed
	}
	tests := []struct {
		name             string
		args             args
		doesMatch        bool
		wantMatches      []*order.MatchSet
		wantNumPassed    int
		wantNumFailed    int
		wantDoneOK       int
		wantNumPartial   int
		wantNumBooked    int
		wantNumUnbooked  int
		wantNumNomatched int
		matchStats       *matcher.MatchCycleStats
	}{
		{
			name: "market sell, 1 maker match",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[0]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				// 1 lot @ 4500000. Leaves best buy of 4300000 behind.
				newMatchSet(takers[0].Order, []*order.LimitOrder{bookBuyOrders[nBuy-1]}),
			},
			wantNumPassed:    1,
			wantNumFailed:    0,
			wantDoneOK:       1,
			wantNumPartial:   0,
			wantNumBooked:    0,
			wantNumUnbooked:  1,
			wantNumNomatched: 0,
			matchStats: &matcher.MatchCycleStats{
				BookBuys:    (bookBuyLots - 1) * LotSize,
				BookSells:   bookSellLots * LotSize,
				MatchVolume: LotSize,
				HighRate:    bookBuyOrders[nBuy-1].Rate,
				LowRate:     bookBuyOrders[nBuy-1].Rate,
				StartRate:   bookBuyOrders[nBuy-1].Rate,
				EndRate:     bookBuyOrders[nBuy-1].Rate,
			},
		},
		{
			name: "market sell, 2 maker match",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[1]}, // 3 lots
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[1].Order, []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2]}),
			},
			wantNumPassed:    1,
			wantNumFailed:    0,
			wantDoneOK:       1,
			wantNumPartial:   0,
			wantNumBooked:    0,
			wantNumUnbooked:  2,
			wantNumNomatched: 0,
			matchStats: &matcher.MatchCycleStats{
				BookBuys:    (bookBuyLots - 3) * LotSize,
				BookSells:   bookSellLots * LotSize,
				MatchVolume: 3 * LotSize,
				HighRate:    bookBuyOrders[nBuy-1].Rate,
				LowRate:     bookBuyOrders[nBuy-2].Rate,
				StartRate:   bookBuyOrders[nBuy-1].Rate,
				EndRate:     bookBuyOrders[nBuy-2].Rate,
			},
			// newLimitOrder(false, 4300000, 2, order.StandingTiF, 0), // older
			// newLimitOrder(false, 4500000, 1, order.StandingTiF, 0),
		},
		{
			name: "market sell, 2 maker match, partial maker fill",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[2]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				// 1 lot @ 4500000, 4 @ 4300000
				newMatchSet(takers[2].Order, []*order.LimitOrder{bookBuyOrders[nBuy-1], bookBuyOrders[nBuy-2], bookBuyOrders[nBuy-3]}, 2*LotSize),
			},
			wantNumPassed:    1,
			wantNumFailed:    0,
			wantDoneOK:       1,
			wantNumPartial:   0,
			wantNumBooked:    0,
			wantNumUnbooked:  2,
			wantNumNomatched: 0,
			matchStats: &matcher.MatchCycleStats{

				BookBuys:    (bookBuyLots - 5) * LotSize,
				BookSells:   bookSellLots * LotSize,
				MatchVolume: 5 * LotSize,

				HighRate:  bookBuyOrders[nBuy-1].Rate,
				LowRate:   bookBuyOrders[nBuy-3].Rate,
				StartRate: bookBuyOrders[nBuy-1].Rate,
				EndRate:   bookBuyOrders[nBuy-3].Rate,
			},
		},
		{
			name: "market sell bad lot size",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{badLotsizeOrder},
			},
			doesMatch:        false,
			wantMatches:      nil,
			wantNumPassed:    0,
			wantNumFailed:    1,
			wantDoneOK:       0,
			wantNumPartial:   0,
			wantNumBooked:    0,
			wantNumUnbooked:  0,
			wantNumNomatched: 0,
		},
		{
			name: "market sell against empty book",
			args: args{
				book:  book.New(LotSize, 0),
				queue: []*matcher.OrderRevealed{takers[0]},
			},
			doesMatch:        false,
			wantMatches:      nil,
			wantNumPassed:    0,
			wantNumFailed:    1,
			wantDoneOK:       0,
			wantNumPartial:   0,
			wantNumBooked:    0,
			wantNumUnbooked:  0,
			wantNumNomatched: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset Filled amounts of all pre-defined orders before each test.
			resetTakers()
			resetMakers()

			//fmt.Printf("%v\n", takers)

			// Ignore the seed since it is tested in the matcher unit tests.
			_, matches, passed, failed, doneOK, partial, booked, nomatched, unbooked, _, stats := me.Match(tt.args.book, tt.args.queue)
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
			if len(doneOK) != tt.wantDoneOK {
				t.Errorf("number doneOK %d, expected %d", len(doneOK), tt.wantDoneOK)
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

			if len(nomatched) != tt.wantNumNomatched {
				t.Errorf("number nomatched %d, expected %d", len(nomatched), tt.wantNumNomatched)
			}
			if tt.matchStats != nil {
				compareMatchStats(t, tt.matchStats, stats)
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

	rand.Seed(0)

	nSell := len(bookSellOrders)
	//nBuy := len(bookBuyOrders)

	// takers is heterogenous w.r.t. type
	takers := []*matcher.OrderRevealed{
		newMarketBuy(quoteAmt(1), 0),  // buy, 1 lot
		newMarketBuy(quoteAmt(2), 0),  // buy, 2 lot
		newMarketBuy(quoteAmt(3), 0),  // buy, 3 lot
		newMarketBuy(quoteAmt(99), 0), // buy, up to 99 lots, computed exactly for the book
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
		book  *book.Book
		queue []*matcher.OrderRevealed
	}
	tests := []struct {
		name            string
		args            args
		doesMatch       bool
		wantMatches     []*order.MatchSet
		remaining       []uint64
		wantNumPassed   int
		wantNumFailed   int
		wantDoneOK      int
		wantNumPartial  int
		wantNumBooked   int
		wantNumUnbooked int
	}{
		{
			name: "market buy, 1 maker match",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[0]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[0].Order, []*order.LimitOrder{bookSellOrders[nSell-1]}),
			},
			remaining:       []uint64{quoteAmt(1) - marketBuyQuoteAmt(1)},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantDoneOK:      1,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 1,
		},
		{
			name: "market buy, 2 maker match",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[1]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[1].Order,
					[]*order.LimitOrder{bookSellOrders[nSell-1], bookSellOrders[nSell-2]},
					1*LotSize),
			},
			remaining:       []uint64{0},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantDoneOK:      1,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 1,
		},
		{
			name: "market buy, 3 maker match",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[2]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[2].Order,
					[]*order.LimitOrder{bookSellOrders[nSell-1], bookSellOrders[nSell-2]}),
			},
			remaining:       []uint64{0},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantDoneOK:      1,
			wantNumPartial:  0,
			wantNumBooked:   0,
			wantNumUnbooked: 2,
		},
		{
			name: "market buy, 99 maker match",
			args: args{
				book:  newBook(t),
				queue: []*matcher.OrderRevealed{takers[3]},
			},
			doesMatch: true,
			wantMatches: []*order.MatchSet{
				newMatchSet(takers[3].Order, bookSellOrdersReverse),
			},
			remaining:       []uint64{0},
			wantNumPassed:   1,
			wantNumFailed:   0,
			wantDoneOK:      1,
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

			// Ignore the seed since it is tested in the matcher unit tests.
			_, matches, passed, failed, doneOK, partial, booked, _, unbooked, _, _ := me.Match(tt.args.book, tt.args.queue)
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

	rand.Seed(12)

	nSell := len(bookSellOrders)
	nBuy := len(bookBuyOrders)
	cancelTime := time.Unix(1566497655, 0)

	// epochQueue is heterogenous w.r.t. type
	epochQueue := []*matcher.OrderRevealed{
		// buys
		newLimit(false, 4550000, 1, order.ImmediateTiF, 0), // 0: buy, 1 lot, immediate
		newLimit(false, 4550000, 2, order.StandingTiF, 0),  // 1: buy, 2 lot, standing
		newLimit(false, 4550000, 2, order.ImmediateTiF, 0), // 2: buy, 2 lot, immediate, unmatched
		newLimit(false, 4100000, 1, order.ImmediateTiF, 0), // 3: buy, 1 lot, immediate, unmatched
		// sells
		newLimit(true, 4540000, 1, order.ImmediateTiF, 0), // 4: sell, 1 lot, immediate
		newLimit(true, 4800000, 4, order.StandingTiF, 0),  // 5: sell, 4 lot, immediate, unmatched
		newLimit(true, 4300000, 4, order.ImmediateTiF, 0), // 6: sell, 4 lot, immediate
		newLimit(true, 4800000, 40, order.StandingTiF, 1), // 7: sell, 40 lot, standing, unfilled insert
		// market
		newMarketSell(2, 0),          // 8
		newMarketSell(4, 0),          // 9
		newMarketBuy(quoteAmt(1), 0), // 10
		newMarketBuy(quoteAmt(2), 0), // 11
		// cancel
		newCancel(bookSellOrders[6].ID(), cancelTime),       // 12
		newCancel(bookBuyOrders[8].ID(), cancelTime),        // 13
		newCancel(bookBuyOrders[nBuy-1].ID(), cancelTime),   // 14
		newCancel(bookSellOrders[nSell-1].ID(), cancelTime), // 15
	}
	// cancel some the epoch queue orders too
	epochQueue = append(epochQueue, newCancel(epochQueue[7].Order.ID(), cancelTime)) // 16 cancels 7 (miss)
	epochQueue = append(epochQueue, newCancel(epochQueue[5].Order.ID(), cancelTime)) // 17 cancels 5 (miss)

	epochQueueInit := make([]*matcher.OrderRevealed, len(epochQueue))
	copy(epochQueueInit, epochQueue)

	// var shuf []int
	// matcher.ShuffleQueue(epochQueue)
	// for i := range epochQueue {
	// 	for j := range epochQueueInit {
	// 		if epochQueue[i].Order.ID() == epochQueueInit[j].Order.ID() {
	// 			shuf = append(shuf, j)
	// 			t.Logf("%d: %p", j, epochQueueInit[j].Order)
	// 			continue
	// 		}
	// 	}
	// }
	// t.Logf("%#v", shuf)

	// Apply the shuffling to determine matching order that will be used.
	// matcher.ShuffleQueue(epochQueue)
	// for i := range epochQueue {
	// 	t.Logf("%d: %p, %p", i, epochQueueInit[i].Order, epochQueue[i].Order)
	// }
	// Shuffles to [6, 13, 0, 14, 11, 10, 7, 1, 12, 5, 17, 4, 16, 2, 9, 8, 15, 3]

	expectedNumMatches := 11
	expectedPassed := []int{5, 0, 8, 10, 13, 6, 17, 9, 7, 16, 12, 1, 4, 11}
	expectedFailed := []int{15, 3, 2, 14}
	expectedDoneOK := []int{0, 8, 10, 13, 6, 17, 9, 16, 12, 4, 11}
	expectedPartial := []int{}
	expectedBooked := []int{5, 7, 1} // all StandingTiF
	expectedNumUnbooked := 8
	expectedNumNomatched := 6

	// order book from bookBuyOrders and bookSellOrders
	b := newBook(t)

	resetQueue := func() {
		for _, o := range epochQueue {
			switch ot := o.Order.(type) {
			case *order.MarketOrder:
				ot.FillAmt = 0
			case *order.LimitOrder:
				ot.FillAmt = 0
			}
		}
	}

	// Reset Filled amounts of all pre-defined orders before each test.
	resetQueue()
	resetMakers()

	// Ignore the seed since it is tested in the matcher unit tests.
	_, matches, passed, failed, doneOK, partial, booked, nomatched, unbooked, _, _ := me.Match(b, epochQueue)
	//t.Log("Matches:", matches)
	// s := "Passed: "
	// for _, o := range passed {
	// 	s += fmt.Sprintf("%p ", o.Order)
	// }
	// t.Log(s)
	// s = "Failed: "
	// for _, o := range failed {
	// 	s += fmt.Sprintf("%p ", o.Order)
	// }
	// t.Log(s)
	// s = "DoneOK: "
	// for _, o := range doneOK {
	// 	s += fmt.Sprintf("%p ", o.Order)
	// }
	// t.Log(s)
	// s = "Partial: "
	// for _, o := range partial {
	// 	s += fmt.Sprintf("%p ", o.Order)
	// }
	// t.Log(s)
	// s = "Booked: "
	// for _, o := range booked {
	// 	s += fmt.Sprintf("%p ", o.Order)
	// }
	// t.Log(s)
	// s := "Nomatched: "
	// for _, o := range nomatched {
	// 	s += fmt.Sprintf("%p ", o.Order)
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

	for i, qi := range expectedDoneOK {
		if oi := orderInSlice(epochQueueInit[qi], doneOK); oi != i {
			t.Errorf("Order not at expected location in doneOK slice. Got %d, expected %d",
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

	if len(unbooked) != expectedNumUnbooked {
		t.Errorf("Incorrect number of unbooked orders. Got %d, expected %d", len(unbooked), expectedNumUnbooked)
	}

	if len(nomatched) != expectedNumNomatched {
		t.Errorf("Incorrect number of nomatched orders. Got %d, expected %d", len(nomatched), expectedNumNomatched)
	}

	if len(matches) != expectedNumMatches {
		t.Errorf("Incorrect number of matches. Got %d, expected %d", len(matches), expectedNumMatches)
	}

	// Spot check a couple of matches.

	// match 4 (epoch order 6) cancels a book order
	if matches[4].Taker.ID() != epochQueueInit[6].Order.ID() {
		t.Errorf("Taker order ID expected %v, got %v",
			epochQueueInit[6].Order.UID(), matches[4].Taker.UID())
	}
	if matches[9].Makers[0].ID() != epochQueueInit[1].Order.ID() {
		t.Errorf("9th match maker was expected to be %v, got %v",
			epochQueueInit[1].Order.UID(), matches[9].Makers[0].ID())
	}
}

func compareMatchStats(t *testing.T, sWant, sHave *matcher.MatchCycleStats) {
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
