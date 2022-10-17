// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package matcher

import (
	"bytes"
	"fmt"
	"math/rand"
	"sort"

	"decred.org/dcrdex/dex/calc"
	"decred.org/dcrdex/dex/order"
	"decred.org/dcrdex/server/matcher/mt19937"
	"github.com/decred/dcrd/crypto/blake256"
)

// HashFunc is the hash function used to generate the shuffling seed.
var (
	HashFunc    = blake256.Sum256
	BaseToQuote = calc.BaseToQuote
	QuoteToBase = calc.QuoteToBase
)

const (
	HashSize = blake256.Size

	peSize = order.PreimageSize
)

type Matcher struct{}

// New creates a new Matcher.
func New() *Matcher {
	return &Matcher{}
}

// orderLotSizeOK checks if the remaining Order quantity is not a multiple of
// lot size, unless the order is a market buy order, which is not subject to
// this constraint.
func orderLotSizeOK(ord order.Order, lotSize uint64) bool {
	var remaining uint64
	switch o := ord.(type) {
	case *order.CancelOrder:
		// NOTE: Cancel orders have 0 remaining by definition.
		return true
	case *order.MarketOrder:
		if !o.Sell {
			return true
		}
		remaining = o.Remaining()
	case *order.LimitOrder:
		remaining = o.Remaining()
	}
	return remaining%lotSize == 0
}

// assertOrderLotSize will panic if the remaining Order quantity is not a
// multiple of lot size, unless the order is a market buy order.
func assertOrderLotSize(ord order.Order, lotSize uint64) {
	if orderLotSizeOK(ord, lotSize) {
		return
	}
	var remaining uint64
	if ord.Trade() != nil {
		remaining = ord.Trade().Remaining()
	}
	panic(fmt.Sprintf(
		"order %v has remaining quantity %d that is not a multiple of lot size %d",
		ord.ID(), remaining, lotSize))
}

// CheckMarketBuyBuffer verifies that the given market buy order's quantity
// (specified in quote asset) is sufficient according to the Matcher's
// configured market buy buffer, which is in base asset units, and the best
// standing sell order according to the provided Booker.
func CheckMarketBuyBuffer(book Booker, ord *order.MarketOrder, marketBuyBuffer float64) bool {
	if ord.Sell {
		return true // The market buy buffer does not apply to sell orders.
	}
	minBaseAsset := uint64(marketBuyBuffer * float64(book.LotSize()))
	return ord.Remaining() >= BaseToQuote(book.BestSell().Rate, minBaseAsset)
}

// OrderRevealed combines an Order interface with a Preimage.
type OrderRevealed struct {
	Order    order.Order // Do not embed so OrderRevealed is not an order.Order.
	Preimage order.Preimage
}

// OrdersUpdated represents the orders updated by (*Matcher).Match, and may
// include orders from both the epoch queue and the book. Trade orders that are
// not in TradesFailed may appear in multiple other trade order slices, so
// update them in sequence: booked, partial, and finally completed or canceled.
// Orders in TradesFailed will not be in another slice since failed indicates
// unmatched&unbooked or bad lot size. Cancel orders may only be in one of
// CancelsExecuted or CancelsFailed.
type OrdersUpdated struct {
	// CancelsExecuted are cancel orders that were matched to and removed
	// another order from the book.
	CancelsExecuted []*order.CancelOrder
	// CancelsFailed are cancel orders that failed to match and cancel an order.
	CancelsFailed []*order.CancelOrder

	// TradesFailed are unmatched and unbooked (i.e. unmatched market or limit
	// with immediate time-in-force), or orders with bad lot size. These orders
	// will be in no other slice.
	TradesFailed []order.Order

	// TradesBooked are limit orders from the epoch queue that were put on the
	// book. Because of downstream epoch processing, these may also be in
	// TradesPartial, and TradesCompleted or TradesCanceled.
	TradesBooked []*order.LimitOrder
	// TradesPartial are limit orders that were partially filled as maker orders
	// (while on the book). Epoch orders that were partially filled prior to
	// being booked (while takers) are not necessarily in TradesPartial unless
	// they are filled again by a taker. This is because all status updates also
	// update the filled amount.
	TradesPartial []*order.LimitOrder
	// TradesCanceled are limit orders that were removed from the the book by a
	// cancel order. These may also be in TradesBooked and TradesPartial, but
	// not TradesCompleted.
	TradesCanceled []*order.LimitOrder
	// TradesCompleted are market or limit orders that filled to completion. For
	// a market order, this means it had a match that partially or completely
	// filled it. For a limit order, this means the time-in-force is immediate
	// with at least one match for any amount, or the time-in-force is standing
	// and it is completely filled.
	TradesCompleted []order.Order
}

func (ou *OrdersUpdated) String() string {
	return fmt.Sprintf("cExec=%d, cFail=%d, tPartial=%d, tBooked=%d, tCanceled=%d, tComp=%d, tFail=%d",
		len(ou.CancelsExecuted), len(ou.CancelsFailed), len(ou.TradesPartial), len(ou.TradesBooked),
		len(ou.TradesCanceled), len(ou.TradesCompleted), len(ou.TradesFailed))
}

// Match matches orders given a standing order book and an epoch queue. Matched
// orders from the book are removed from the book. The EpochID of the MatchSet
// is not set. passed = booked + doneOK. queue = passed + failed. unbooked may
// include orders that are not in the queue. Each of partial are in passed.
// nomatched are orders that did not match anything, and discludes booked
// limit orders that only matched as makers to down-queue takers.
//
// TODO: Eliminate order slice return args in favor of just the *OrdersUpdated.
func (m *Matcher) Match(book Booker, queue []*OrderRevealed) (seed []byte, matches []*order.MatchSet,
	passed, failed, doneOK, partial, booked, nomatched []*OrderRevealed,
	unbooked []*order.LimitOrder, updates *OrdersUpdated, stats *MatchCycleStats) {

	// Apply the deterministic pseudorandom shuffling.
	seed = shuffleQueue(queue)

	updates = new(OrdersUpdated)
	stats = new(MatchCycleStats)

	appendTradeSet := func(matchSet *order.MatchSet) {
		matches = append(matches, matchSet)

		stats.MatchVolume += matchSet.Total
		high, low := matchSet.HighLowRates()
		if high > stats.HighRate {
			stats.HighRate = high
		}
		if low < stats.LowRate || stats.LowRate == 0 {
			stats.LowRate = low
		}
		stats.QuoteVolume += matchSet.QuoteVolume()
	}

	// Store partially filled limit orders in a map to avoid duplicate
	// entries.
	partialMap := make(map[order.OrderID]*order.LimitOrder)

	// Track initially unmatched standing limit orders and remove them if they
	// are matched down-queue so that they aren't added to the nomatched slice.
	nomatchStanding := make(map[order.OrderID]*OrderRevealed)

	tallyMakers := func(makers []*order.LimitOrder) {
		for _, maker := range makers {
			delete(nomatchStanding, maker.ID())
			if maker.Remaining() == 0 {
				unbooked = append(unbooked, maker)
				updates.TradesCompleted = append(updates.TradesCompleted, maker)
				delete(partialMap, maker.ID())
			} else {
				partialMap[maker.ID()] = maker
			}
		}
	}

	// For each order in the queue, find the best match in the book.
	for _, q := range queue {
		if !orderLotSizeOK(q.Order, book.LotSize()) {
			log.Warnf("Order with bad lot size in the queue: %v!", q.Order.ID())
			failed = append(failed, q)
			updates.TradesFailed = append(updates.TradesFailed, q.Order)
			continue
		}

		switch o := q.Order.(type) {
		case *order.CancelOrder:
			removed, ok := book.Remove(o.TargetOrderID)
			if !ok {
				// The targeted order might be down queue or non-existent.
				log.Debugf("Order %v not removed by a cancel order %v (target either non-existent or down queue in this epoch)",
					o.ID(), o.TargetOrderID)
				failed = append(failed, q)
				updates.CancelsFailed = append(updates.CancelsFailed, o)
				nomatched = append(nomatched, q)
				continue
			}

			passed = append(passed, q)
			doneOK = append(doneOK, q)
			updates.CancelsExecuted = append(updates.CancelsExecuted, o)

			// CancelOrder Match has zero values for Amounts, Rates, and Total.
			matches = append(matches, &order.MatchSet{
				Taker:   q.Order,
				Makers:  []*order.LimitOrder{removed},
				Amounts: []uint64{removed.Remaining()},
				Rates:   []uint64{removed.Rate},
			})
			unbooked = append(unbooked, removed)
			updates.TradesCanceled = append(updates.TradesCanceled, removed)

		case *order.LimitOrder:
			// limit-limit order matching
			var makers []*order.LimitOrder
			matchSet := matchLimitOrder(book, o)

			if matchSet != nil {
				appendTradeSet(matchSet)
				makers = matchSet.Makers
			} else {
				if o.Force == order.ImmediateTiF {
					nomatched = append(nomatched, q)
					// There was no match and TiF is Immediate. Fail.
					failed = append(failed, q)
					updates.TradesFailed = append(updates.TradesFailed, o)
					break
				} else {
					nomatchStanding[q.Order.ID()] = q
				}
			}

			// Either matched or standing unmatched => passed.
			passed = append(passed, q)

			tallyMakers(makers)

			var wasBooked bool
			if o.Remaining() > 0 {
				if o.Filled() > 0 {
					partial = append(partial, q)
				}
				if o.Force == order.StandingTiF {
					// Standing TiF orders go on the book.
					book.Insert(o)
					booked = append(booked, q)
					updates.TradesBooked = append(updates.TradesBooked, o)
					wasBooked = true
				}
			}

			// booked => TradesBooked
			// !booked => TradesCompleted

			if !wasBooked { // either nothing remaining or immediate force
				doneOK = append(doneOK, q)
				updates.TradesCompleted = append(updates.TradesCompleted, o)
			}

		case *order.MarketOrder:
			// market-limit order matching
			var matchSet *order.MatchSet

			if o.Sell {
				matchSet = matchMarketSellOrder(book, o)
			} else {
				// Market buy order Quantity is denominated in the quote asset,
				// and lot size multiples are not applicable.
				matchSet = matchMarketBuyOrder(book, o)
			}
			if matchSet != nil {
				// Only count market order volume that matches.
				appendTradeSet(matchSet)
				passed = append(passed, q)
				doneOK = append(doneOK, q)
				updates.TradesCompleted = append(updates.TradesCompleted, o)
			} else {
				// There was no match and this is a market order. Fail.
				failed = append(failed, q)
				updates.TradesFailed = append(updates.TradesFailed, o)
				nomatched = append(nomatched, q)
				break
			}

			tallyMakers(matchSet.Makers)

			// Regardless of remaining amount, market orders never go on the book.
		}

	}

	for _, lo := range partialMap {
		updates.TradesPartial = append(updates.TradesPartial, lo)
	}

	for _, q := range nomatchStanding {
		nomatched = append(nomatched, q)
	}

	for _, matchSet := range matches {
		if matchSet.Total > 0 { // cancel filter
			stats.StartRate = matchSet.Makers[0].Rate
			break
		}
	}
	if stats.StartRate > 0 { // If we didn't find anything going forward, no need to check going backwards.
		for i := len(matches) - 1; i >= 0; i-- {
			matchSet := matches[i]
			if matchSet.Total > 0 { // cancel filter
				stats.EndRate = matchSet.Makers[len(matchSet.Makers)-1].Rate
				break
			}
		}
	}

	bookVolumes(book, stats)

	return
}

// limit-limit order matching
func matchLimitOrder(book Booker, ord *order.LimitOrder) (matchSet *order.MatchSet) {
	amtRemaining := ord.Remaining() // i.e. ord.Quantity - ord.FillAmt
	if amtRemaining == 0 {
		return
	}

	lotSize := book.LotSize()
	assertOrderLotSize(ord, lotSize)

	bestFunc := book.BestSell
	rateMatch := func(b, s uint64) bool { return s <= b }
	if ord.Sell {
		// order is a sell order
		bestFunc = book.BestBuy
		rateMatch = func(s, b uint64) bool { return s <= b }
	}

	// Find matches until the order has been depleted.
	for amtRemaining > 0 {
		// Get the best book order for this limit order.
		best := bestFunc() // maker
		if best == nil {
			return
		}
		assertOrderLotSize(best, lotSize)

		// Check rate.
		if !rateMatch(ord.Rate, best.Rate) {
			return
		}
		// now, best.Rate <= ord.Rate

		// The match amount is the smaller of the order's remaining quantity or
		// the best matching order amount.
		amt := best.Remaining()
		if amtRemaining < amt {
			// Partially fill the standing order, updating its value.
			amt = amtRemaining
		} else {
			// The standing order has been consumed. Remove it from the book.
			if _, ok := book.Remove(best.ID()); !ok {
				log.Errorf("Failed to remove standing order %v.", best)
			}
		}
		best.AddFill(amt)

		// Reduce the remaining quantity of the taker order.
		amtRemaining -= amt
		ord.AddFill(amt)

		// Add the matched maker order to the output.
		if matchSet == nil {
			matchSet = &order.MatchSet{
				Taker:   ord,
				Makers:  []*order.LimitOrder{best},
				Amounts: []uint64{amt},
				Rates:   []uint64{best.Rate},
				Total:   amt,
			}
		} else {
			matchSet.Makers = append(matchSet.Makers, best)
			matchSet.Amounts = append(matchSet.Amounts, amt)
			matchSet.Rates = append(matchSet.Rates, best.Rate)
			matchSet.Total += amt
		}
	}

	return
}

// market(sell)-limit order matching
func matchMarketSellOrder(book Booker, ord *order.MarketOrder) (matchSet *order.MatchSet) {
	if !ord.Sell {
		panic("matchMarketSellOrder: not a sell order")
	}

	// A market sell order is a special case of a limit order with time-in-force
	// immediate and no minimum rate (a rate of 0).
	limOrd := &order.LimitOrder{
		P:     ord.P,
		T:     *ord.T.Copy(),
		Force: order.ImmediateTiF,
		Rate:  0,
	}
	matchSet = matchLimitOrder(book, limOrd)
	if matchSet == nil {
		return
	}
	// The Match.Taker must be the *MarketOrder, not the wrapped *LimitOrder.
	matchSet.Taker = ord
	return
}

// market(buy)-limit order matching
func matchMarketBuyOrder(book Booker, ord *order.MarketOrder) (matchSet *order.MatchSet) {
	if ord.Sell {
		panic("matchMarketBuyOrder: not a buy order")
	}

	lotSize := book.LotSize()

	// Amount remaining for market buy is in *quote* asset, not base asset.
	amtRemaining := ord.Remaining() // i.e. ord.Quantity - ord.FillAmt
	if amtRemaining == 0 {
		return
	}

	// Find matches until the order has been depleted.
	for amtRemaining > 0 {
		// Get the best book order for this limit order.
		best := book.BestSell() // maker
		if best == nil {
			return
		}

		// Convert the market buy order's quantity into base asset:
		//   quoteAmt = rate * baseAmt
		amtRemainingBase := QuoteToBase(best.Rate, amtRemaining)
		//amtRemainingBase := uint64(float64(amtRemaining) / best.Rate) // trunc
		if amtRemainingBase < lotSize {
			return
		}

		// To convert the matching limit order's quantity into quote asset:
		// amt := uint64(best.Rate * float64(best.Quantity)) // trunc

		// The match amount is the smaller of the order's remaining quantity or
		// the best matching order amount.
		amt := best.Remaining()
		if amtRemainingBase < amt {
			// Partially fill the standing order, updating its value.
			amt = amtRemainingBase - amtRemainingBase%lotSize // amt is a multiple of lot size
		} else {
			// The standing order has been consumed. Remove it from the book.
			if _, ok := book.Remove(best.ID()); !ok {
				log.Errorf("Failed to remove standing order %v.", best)
			}
		}
		best.AddFill(amt)

		// Reduce the remaining quantity of the taker order.
		// amtRemainingBase -= amt // FYI
		amtQuote := BaseToQuote(best.Rate, amt)
		//amtQuote := uint64(float64(amt) * best.Rate)
		amtRemaining -= amtQuote // quote asset remaining
		ord.AddFill(amtQuote)    // quote asset filled

		// Add the matched maker order to the output.
		if matchSet == nil {
			matchSet = &order.MatchSet{
				Taker:   ord,
				Makers:  []*order.LimitOrder{best},
				Amounts: []uint64{amt},
				Rates:   []uint64{best.Rate},
				Total:   amt,
			}
		} else {
			matchSet.Makers = append(matchSet.Makers, best)
			matchSet.Amounts = append(matchSet.Amounts, amt)
			matchSet.Rates = append(matchSet.Rates, best.Rate)
			matchSet.Total += amt
		}
	}

	return
}

// OrdersMatch checks if two orders are valid matches, without regard to quantity.
// - not a cancel order
// - not two market orders
// - orders on different sides (one buy and one sell)
// - two limit orders with overlapping rates, or one market and one limit
func OrdersMatch(a, b order.Order) bool {
	// Get order data needed for comparison.
	aType, aSell, _, aRate := orderData(a)
	bType, bSell, _, bRate := orderData(b)

	// Orders must be on opposite sides of the market.
	if aSell == bSell {
		return false
	}

	// Screen order types.
	switch aType {
	case order.MarketOrderType:
		switch bType {
		case order.LimitOrderType:
			return true // market-limit
		case order.MarketOrderType:
			fallthrough // no two market orders
		default:
			return false // cancel or unknown
		}
	case order.LimitOrderType:
		switch bType {
		case order.LimitOrderType:
			// limit-limit: must check rates
		case order.MarketOrderType:
			return true // limit-market
		default:
			return false // cancel or unknown
		}
	default: // cancel or unknown
		return false
	}

	// For limit-limit orders, check that the rates overlap.
	cmp := func(buyRate, sellRate uint64) bool { return sellRate <= buyRate }
	if bSell {
		// a is buy, b is sell
		return cmp(aRate, bRate)
	}
	// a is sell, b is buy
	return cmp(bRate, aRate)
}

func orderData(o order.Order) (orderType order.OrderType, sell bool, amount, rate uint64) {
	orderType = o.Type()

	switch ot := o.(type) {
	case *order.LimitOrder:
		sell = ot.Sell
		amount = ot.Quantity
		rate = ot.Rate
	case *order.MarketOrder:
		sell = ot.Sell
		amount = ot.Quantity
	}

	return
}

// sortQueueByCommit lexicographically sorts the Orders by their commitments.
// This is used to compute the commitment checksum, which is sent to the clients
// in the preimage requests prior to queue shuffling. There must not be
// duplicated commitments.
func sortQueueByCommit(queue []order.Order) {
	sort.Slice(queue, func(i, j int) bool {
		ii, ij := queue[i].Commitment(), queue[j].Commitment()
		return bytes.Compare(ii[:], ij[:]) < 0
	})
}

// CSum computes the commitment checksum for the order queue. For an empty
// queue, the result is a nil slice instead of the initial hash state.
func CSum(queue []order.Order) []byte {
	if len(queue) == 0 {
		return nil
	}
	sortQueueByCommit(queue)
	hasher := blake256.New()
	for _, ord := range queue {
		commit := ord.Commitment()
		hasher.Write(commit[:]) // err is always nil and n is always len(s)
	}
	return hasher.Sum(nil)
}

// sortQueueByID lexicographically sorts the Orders by their IDs. Note that
// while sorting is done with the order ID, the preimage is still used to
// specify the shuffle order. The result is undefined if the slice contains
// duplicated order IDs.
func sortQueueByID(queue []*OrderRevealed) {
	sort.Slice(queue, func(i, j int) bool {
		ii, ij := queue[i].Order.ID(), queue[j].Order.ID()
		return bytes.Compare(ii[:], ij[:]) < 0
	})
}

func ShuffleQueue(queue []*OrderRevealed) {
	shuffleQueue(queue)
}

// shuffleQueue deterministically shuffles the Orders using a Fisher-Yates
// algorithm seeded with the hash of the concatenated order commitment
// preimages. If any orders in the queue are repeated, the order sorting
// behavior is undefined.
func shuffleQueue(queue []*OrderRevealed) (seed []byte) {
	// Nothing to do if there are no orders. For one order, the seed must still
	// be computed.
	if len(queue) == 0 {
		return
	}

	// The shuffling seed is derived from the concatenation of the order
	// preimages, lexicographically sorted by order ID.
	sortQueueByID(queue)

	// Hash the concatenation of the preimages.
	qLen := len(queue)
	hasher := blake256.New()
	//peCat := make([]byte, peSize*qLen)
	for _, o := range queue {
		hasher.Write(o.Preimage[:]) // err is always nil and n is always len(s)
		//copy(peCat[peSize*i:peSize*(i+1)], o.Preimage[:])
	}

	// Fisher-Yates shuffle the slice using MT19937 seeded with the hash.
	seed = hasher.Sum(nil)
	// seed = HashFunc(hashCat)

	// This seeded random number generator is used to generate one sequence, and
	// the seed is revealed then revealed. It need not be cryptographically
	// secure.
	mtSrc := mt19937.NewSource()
	mtSrc.SeedBytes(seed[:])
	prng := rand.New(mtSrc)
	for i := range queue {
		j := prng.Intn(qLen-i) + i
		queue[i], queue[j] = queue[j], queue[i]
	}

	return
}

func midGap(book Booker) uint64 {
	b, s := book.BestBuy(), book.BestSell()
	if b == nil {
		if s == nil {
			return 0
		}
		return s.Rate
	} else if s == nil {
		return b.Rate
	}
	return (b.Rate + s.Rate) / 2
}

func sideVolume(ords []*order.LimitOrder) (q uint64) {
	for _, ord := range ords {
		q += ord.Remaining()
	}
	return
}

func bookVolumes(book Booker, stats *MatchCycleStats) {
	midGap := midGap(book)
	cutoff5 := midGap - midGap/20 // 5%
	cutoff25 := midGap - midGap/4 // 25%
	for _, ord := range book.BuyOrders() {
		remaining := ord.Remaining()
		stats.BookBuys += remaining
		if ord.Rate > cutoff25 {
			stats.BookBuys25 += remaining
			if ord.Rate > cutoff5 {
				stats.BookBuys5 += remaining
			}
		}
	}
	cutoff5 = midGap + midGap/20
	cutoff25 = midGap + midGap/4
	for _, ord := range book.SellOrders() {
		remaining := ord.Remaining()
		stats.BookSells += remaining
		if ord.Rate < cutoff25 {
			stats.BookSells25 += remaining
			if ord.Rate < cutoff5 {
				stats.BookSells5 += remaining
			}
		}
	}
}
