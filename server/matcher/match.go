// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package matcher

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"
	"math/rand"
	"sort"

	"github.com/decred/dcrd/crypto/blake256"
	"github.com/decred/dcrdex/server/market/order"
)

// HashFunc is the hash function used to generate the shuffling seed.
var HashFunc = blake256.Sum256

const (
	HashSize = blake256.Size

	atomsPerCoin uint64 = 1e8
)

var bigAtomsPerCoin = big.NewInt(int64(atomsPerCoin))

type Matcher struct{}

// New creates a new Matcher.
func New() *Matcher {
	return &Matcher{}
}

// orderLotSizeOK checks if the remaining Order quantity is not a multiple of
// lot size, unless the order is a market buy order, which is not subject to
// this constraint.
func orderLotSizeOK(ord order.Order, lotSize uint64) bool {
	if mo, ok := ord.(*order.MarketOrder); ok {
		// Market buy orders are not subject to lot size constraints.
		if !mo.Sell {
			return true
		}
	}

	// NOTE: Cancel orders have 0 remaining by definition.
	return ord.Remaining()%lotSize == 0
}

// assertOrderLotSize will panic if the remaining Order quantity is not a
// multiple of lot size, unless the order is a market buy order.
func assertOrderLotSize(ord order.Order, lotSize uint64) {
	if orderLotSizeOK(ord, lotSize) {
		return
	}
	panic(fmt.Sprintf(
		"order %v has remaining quantity %d that is not a multiple of lot size %d",
		ord.ID(), ord.Remaining(), lotSize))
}

// BaseToQuote computes a quote asset amount based on a base asset amount
// and an integer representation of the price rate. That is,
//    quoteAmt = rate * baseAmt / atomsPerCoin
func BaseToQuote(rate uint64, base uint64) (quote uint64) {
	bigRate := big.NewInt(int64(rate))
	bigBase := big.NewInt(int64(base))
	bigBase.Mul(bigBase, bigRate)
	bigBase.Div(bigBase, bigAtomsPerCoin)
	return bigBase.Uint64()
}

// QuoteToBase computes a base asset amount based on a quote asset amount
// and an integer representation of the price rate. That is,
//    baseAmt = quoteAmt * atomsPerCoin / rate
func QuoteToBase(rate uint64, quote uint64) (base uint64) {
	bigRate := big.NewInt(int64(rate))
	bigQuote := big.NewInt(int64(quote))
	bigQuote.Mul(bigQuote, bigAtomsPerCoin)
	bigQuote.Div(bigQuote, bigRate)
	return bigQuote.Uint64()
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

// Match matches orders given a standing order book and an epoch queue. Matched
// orders from the book are removed from the book.
func (m *Matcher) Match(book Booker, queue []order.Order) (matches []*order.Match, passed, failed, partial, inserted []order.Order) {
	// Apply the deterministic pseudorandom shuffling.
	shuffleQueue(queue)

	// For each order in the queue, find the best match in the book.
	for _, q := range queue {
		if !orderLotSizeOK(q, book.LotSize()) {
			log.Warnf("Order with bad lot size in the queue: %v!", q.ID())
			failed = append(failed, q)
			continue
		}

		switch o := q.(type) {
		case *order.CancelOrder:
			removed, ok := book.Remove(o.TargetOrderID)
			if !ok {
				// The targeted order might be down queue or non-existent.
				log.Debugf("Failed to remove order %v set by a cancel order %v",
					o.ID(), o.TargetOrderID)
				failed = append(failed, q)
				continue
			}

			passed = append(passed, q)
			// CancelOrder Match has zero values for Amounts, Rates, and Total.
			matches = append(matches, &order.Match{
				Taker:  q,
				Makers: []*order.LimitOrder{removed},
			})

		case *order.LimitOrder:
			// limit-limit order matching
			match := matchLimitOrder(book, o)
			if match != nil {
				matches = append(matches, match)
				passed = append(passed, q)
			} else if o.Force == order.ImmediateTiF {
				// There was no match and TiF is Immediate. Fail.
				failed = append(failed, q)
				break
			}

			if o.Remaining() > 0 {
				partial = append(partial, q)
				if o.Force == order.StandingTiF {
					// Standing TiF orders go on the book.
					book.Insert(o)
					inserted = append(inserted, q)
				}
			}

		case *order.MarketOrder:
			// market-limit order matching
			var match *order.Match
			if o.Sell {
				match = matchMarketSellOrder(book, o)
			} else {
				// Market buy order Quantity is denominated in the quote asset,
				// and lot size multiples are not applicable.
				match = matchMarketBuyOrder(book, o)
			}
			if match != nil {
				matches = append(matches, match)
				passed = append(passed, q)
			} else {
				// There was no match and this is a market order. Fail.
				failed = append(failed, q)
			}

			// Regardless of remaining amount, market orders never go on the book.
		}

	}

	return
}

// limit-limit order matching
func matchLimitOrder(book Booker, ord *order.LimitOrder) (match *order.Match) {
	amtRemaining := ord.Remaining() // i.e. ord.Quantity - ord.Filled
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
		best.Filled += amt

		// Reduce the remaining quantity of the taker order.
		amtRemaining -= amt
		ord.Filled += amt

		// Add the matched maker order to the output.
		if match == nil {
			match = &order.Match{
				Taker:   ord,
				Makers:  []*order.LimitOrder{best},
				Amounts: []uint64{amt},
				Rates:   []uint64{best.Rate},
				Total:   amt,
			}
		} else {
			match.Makers = append(match.Makers, best)
			match.Amounts = append(match.Amounts, amt)
			match.Rates = append(match.Rates, best.Rate)
			match.Total += amt
		}
	}

	return
}

// market(sell)-limit order matching
func matchMarketSellOrder(book Booker, ord *order.MarketOrder) (match *order.Match) {
	if !ord.Sell {
		panic("matchMarketSellOrder: not a sell order")
	}

	// A market sell order is a special case of a limit order with time-in-force
	// immediate and no minimum rate (a rate of 0).
	limOrd := &order.LimitOrder{
		MarketOrder: *ord,
		Force:       order.ImmediateTiF,
		Rate:        0,
	}
	match = matchLimitOrder(book, limOrd)
	if match == nil {
		return
	}
	// The Match.Taker must be the *MarketOrder, not the wrapped *LimitOrder.
	match.Taker = ord
	return
}

// market(buy)-limit order matching
func matchMarketBuyOrder(book Booker, ord *order.MarketOrder) (match *order.Match) {
	if ord.Sell {
		panic("matchMarketBuyOrder: not a buy order")
	}

	lotSize := book.LotSize()

	// Amount remaining for market buy is in *quoute* asset, not base asset.
	amtRemaining := ord.Remaining() // i.e. ord.Quantity - ord.Filled
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
		best.Filled += amt

		// Reduce the remaining quantity of the taker order.
		// amtRemainingBase -= amt // FYI
		amtQuote := BaseToQuote(best.Rate, amt)
		//amtQuote := uint64(float64(amt) * best.Rate)
		amtRemaining -= amtQuote // quote asset remaining
		ord.Filled += amtQuote   // quote asset filled

		// Add the matched maker order to the output.
		if match == nil {
			match = &order.Match{
				Taker:   ord,
				Makers:  []*order.LimitOrder{best},
				Amounts: []uint64{amt},
				Rates:   []uint64{best.Rate},
				Total:   amt,
			}
		} else {
			match.Makers = append(match.Makers, best)
			match.Amounts = append(match.Amounts, amt)
			match.Rates = append(match.Rates, best.Rate)
			match.Total += amt
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

// sortQueue lexicographically sorts the Orders by their IDs.
func sortQueue(queue []order.Order) {
	sort.Slice(queue, func(i, j int) bool {
		ii, ij := queue[i].ID(), queue[j].ID()
		return bytes.Compare(ii[:], ij[:]) >= 0
	})
}

// shuffleQueue deterministically shuffles the Orders using a Fisher-Yates
// algorithm seeded with the hash of the concatenated order ID hashes.
func shuffleQueue(queue []order.Order) {
	// The shuffling seed is derived from the sorted orders.
	sortQueue(queue)

	// Compute and concatenate the hashes of the order IDs.
	qLen := len(queue)
	hashCat := make([]byte, HashSize*qLen)
	for i, o := range queue {
		id := o.ID()
		h := HashFunc(id[:])
		copy(hashCat[HashSize*i:HashSize*(i+1)], h[:])
	}

	// Fisher-Yates shuffle the slice using a seed derived from the hash of the
	// concatenated order ID hashes.
	seedHash := HashFunc(hashCat)
	seed := int64(binary.LittleEndian.Uint64(seedHash[:8]))
	prng := rand.New(rand.NewSource(seed))
	for i := range queue {
		j := prng.Intn(qLen-i) + i
		queue[i], queue[j] = queue[j], queue[i]
	}
}
