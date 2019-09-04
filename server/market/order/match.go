// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package order

// Match represents the result of matching a single Taker order from the epoch
// queue with one or more standing limit orders from the book, the Makers. The
// Amounts and Rates of each limit order paired with the taker order are stored.
// The Rates slice is for convenience as each rate must match with the Maker's
// rates. However, one of the Amounts may be less than the full quantity of the
// corresponding limit order, indicating a partial fill of the Maker. The sum of
// the amounts, Total, is provided for convenience.
type Match struct {
	Taker   Order
	Makers  []*LimitOrder
	Amounts []uint64
	Rates   []uint64
	Total   uint64
}
