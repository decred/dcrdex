package book

import (
	"testing"

	"decred.org/dcrdex/dex/order"
)

func TestAccounts(t *testing.T) {
	sellerTo := "sellerTo"
	sellerFrom := "sellerFrom"
	loSell := newLimitOrder(true, 1e8, 10, order.StandingTiF, 0)
	loSell.Coins = []order.CoinID{[]byte(sellerFrom)}
	loSell.Address = sellerTo

	seller2To := "seller2To"
	seller2From := "seller2From"
	loSell2 := newLimitOrder(true, 1e8, 10, order.StandingTiF, 0)
	loSell2.Coins = []order.CoinID{[]byte(seller2From)}
	loSell2.Address = seller2To

	buyerTo := "buyerTo"
	buyerFrom := "buyerFrom"
	loBuy := newLimitOrder(false, 1e8, 10, order.StandingTiF, 0)
	loBuy.Coins = []order.CoinID{[]byte(buyerFrom)}
	loBuy.Address = buyerTo

	// Same buyer, other order
	loBuy2 := newLimitOrder(false, 2e8, 10, order.StandingTiF, 0)
	loBuy2.Coins = []order.CoinID{[]byte(buyerFrom)}
	loBuy2.Address = buyerTo

	allAddrs := []string{sellerTo, sellerFrom, buyerTo, buyerFrom, seller2From, seller2To}
	allOrds := []*order.LimitOrder{loSell, loSell2, loBuy, loBuy2}

	ensureCount := func(tag string, addr string, n int, counter func(string, func(*order.LimitOrder))) {
		t.Helper()
		var count int
		counter(addr, func(*order.LimitOrder) {
			count++
		})
		if n != count {
			t.Fatalf("%s: wrong %s base asset count: wanted %d, got %d", tag, addr, n, count)
		}
	}

	ensureBaseCount := func(tag string, tracker *accountTracker, addr string, n int) {
		t.Helper()
		ensureCount(tag, addr, n, tracker.iterateBaseAccount)
	}

	ensureQuoteCount := func(tag string, tracker *accountTracker, addr string, n int) {
		t.Helper()
		ensureCount(tag, addr, n, tracker.iterateQuoteAccount)
	}

	type test struct {
		name           string
		tracker        *accountTracker
		expBaseCounts  map[string]int
		expQuoteCounts map[string]int
	}

	runTests := func(tests []*test, prep func(tracker *accountTracker)) {
		for _, tt := range tests {
			if tt.expBaseCounts == nil {
				tt.expBaseCounts = make(map[string]int)
			}
			if tt.expQuoteCounts == nil {
				tt.expQuoteCounts = make(map[string]int)
			}
			prep(tt.tracker)
			for _, acctAddr := range allAddrs {
				ensureBaseCount(tt.name, tt.tracker, acctAddr, tt.expBaseCounts[acctAddr])
				ensureQuoteCount(tt.name, tt.tracker, acctAddr, tt.expQuoteCounts[acctAddr])
			}
		}
	}

	tests := []*test{
		{
			name:    "non-tracker",
			tracker: newAccountTracker(0),
		},
		{
			name:    "base-tracker",
			tracker: newAccountTracker(AccountTrackingBase),
			expBaseCounts: map[string]int{
				buyerTo:     2,
				sellerFrom:  1,
				seller2From: 1,
			},
		},
		{
			name:    "quote-tracker",
			tracker: newAccountTracker(AccountTrackingQuote),
			expQuoteCounts: map[string]int{
				buyerFrom: 2,
				sellerTo:  1,
				seller2To: 1,
			},
		},
		{
			name:    "both-tracker",
			tracker: newAccountTracker(AccountTrackingQuote | AccountTrackingBase),
			expQuoteCounts: map[string]int{
				buyerFrom: 2,
				sellerTo:  1,
				seller2To: 1,
			},
			expBaseCounts: map[string]int{
				buyerTo:     2,
				sellerFrom:  1,
				seller2From: 1,
			},
		},
	}

	runTests(tests, func(tracker *accountTracker) {
		for _, ord := range allOrds {
			tracker.add(ord)
		}
	})

	// For the next tests, we'll add all of the orders, but then remove one of
	// the buyer's orders.
	tests = []*test{
		{
			name:    "remove-a-buy",
			tracker: newAccountTracker(AccountTrackingBase | AccountTrackingQuote),
			expQuoteCounts: map[string]int{
				buyerFrom: 1,
				sellerTo:  1,
				seller2To: 1,
			},
			expBaseCounts: map[string]int{
				buyerTo:     1,
				sellerFrom:  1,
				seller2From: 1,
			},
		},
	}

	runTests(tests, func(tracker *accountTracker) {
		for _, ord := range allOrds {
			tracker.add(ord)
		}
		tracker.remove(loBuy2)
	})

	// Add all, remove all, and make sure the book is empty.
	tests = []*test{
		{
			name:    "remove-all",
			tracker: newAccountTracker(AccountTrackingBase | AccountTrackingQuote),
		},
	}
	runTests(tests, func(tracker *accountTracker) {
		for _, ord := range allOrds {
			tracker.add(ord)
		}
		for _, ord := range allOrds {
			tracker.remove(ord)
		}
	})
}
