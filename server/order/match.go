// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package order

import (
	"encoding/binary"
	"encoding/hex"

	"github.com/decred/dcrd/crypto/blake256"
)

// MatchIDSize defines the length in bytes of an MatchID.
const MatchIDSize = blake256.Size

// MatchID is the unique identifier for each match.
type MatchID [MatchIDSize]byte

var zeroID = MatchID{}

// MatchStatus represents the current negotiation step for a match.
type MatchStatus uint8

// MatchID implements fmt.Stringer.
func (id MatchID) String() string {
	return hex.EncodeToString(id[:])
}

// The different states of order execution.
const (
	// NewlyMatched: DEX has sent match notifications, but the maker has not yet
	// acted.
	NewlyMatched MatchStatus = iota // 0
	// MakerSwapCast: Maker has acknowledged their match notification and
	// broadcast their swap notification. The DEX has validated the swap
	// notification and sent the details to the taker.
	MakerSwapCast // 1
	// TakerSwapCast: Taker has acknowledged their match notification and
	// broadcast their swap notification. The DEX has validated the swap
	// notification and sent the details to the maker.
	TakerSwapCast // 2
	// MakerRedeemed: Maker has acknowledged their audit request and
	// broadcast their redemption transaction. The DEX has validated the
	// redemption and sent the details to the taker.
	MakerRedeemed // 3
	// MatchComplete: Taker has acknowledged their audit request and
	// broadcast their redemption transaction. The DEX has validated the
	// redemption and sent the details to the maker.
	MatchComplete // 4
)

// MatchSide is the client's side in a match. It will be one of Maker or Taker.
type MatchSide uint8

const (
	// Maker is the order that matches out of the epoch queue.
	Maker = iota
	// Taker is the order from the order book.
	Taker
)

// Signatures holds the acknowledgement signatures required for swap
// negotiation.
type Signatures struct {
	TakerMatch  []byte
	MakerMatch  []byte
	TakerAudit  []byte
	MakerAudit  []byte
	TakerRedeem []byte
	MakerRedeem []byte
}

// Match represents a match between two orders.
type Match struct {
	Taker      Order
	Maker      *LimitOrder
	Quantity   uint64
	Rate       uint64
	Status     MatchStatus
	Sigs       Signatures
	cachedHash MatchID
}

// A UserMatch is similar to a match, but contains less information about the
// counter-party, and is clarifies which side the user is on. This is the
// information that might be provided to the client when they are resyncing
// their matches after a reconnect.
type UserMatch struct {
	OrderID  OrderID
	MatchID  MatchID
	Quantity uint64
	Rate     uint64
	Address  string
	Time     uint64
	Status   MatchStatus
	Side     MatchSide
}

// A constructor for a Match with Status = NewlyMatched. This is the preferred
// method of making a Match, since it pre-calculates and caches the match ID.
func newMatch(taker Order, maker *LimitOrder, qty, rate uint64) *Match {
	m := &Match{
		Taker:    taker,
		Maker:    maker,
		Quantity: qty,
		Rate:     rate,
	}
	// Pre-cache the ID.
	m.ID()
	return m
}

// The match ID.
// BLAKE256([maker order id] + [taker order id] + [match qty] + [match rate])
func (match *Match) ID() MatchID {
	if match.cachedHash != zeroID {
		return match.cachedHash
	}
	b := make([]byte, 0, 2*OrderIDSize+8+8)
	b = appendOrderID(b, match.Taker)
	b = appendOrderID(b, match.Maker)
	b = appendUint64Bytes(b, match.Quantity)
	b = appendUint64Bytes(b, match.Rate)
	match.cachedHash = blake256.Sum256(b)
	return match.cachedHash
}

// MatchSet represents the result of matching a single Taker order from the
// epoch queue with one or more standing limit orders from the book, the Makers.
// The Amounts and Rates of each standing order paired are stored. The Rates
// slice is for convenience since each rate must be the same as the Maker's
// rate. However, a amount in Amounts may be less than the full quantity of the
// corresponding Maker order, indicating a partial fill of the Maker. The sum
// of the amounts, Total, is provided for convenience.
type MatchSet struct {
	Taker   Order
	Makers  []*LimitOrder
	Amounts []uint64
	Rates   []uint64
	Total   uint64
}

// Matches converts the MatchSet to a []*Match.
func (set *MatchSet) Matches() []*Match {
	matches := make([]*Match, 0, len(set.Makers))
	for i, maker := range set.Makers {
		matches = append(matches, newMatch(set.Taker, maker, set.Amounts[i], set.Rates[i]))
	}
	return matches
}

func appendUint64Bytes(b []byte, i uint64) []byte {
	iBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(iBytes, i)
	return append(b, iBytes...)
}

func appendOrderID(b []byte, order Order) []byte {
	oid := order.ID()
	return append(b, oid[:]...)
}
