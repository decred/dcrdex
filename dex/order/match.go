// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package order

import (
	"database/sql/driver"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"time"

	"decred.org/dcrdex/dex/encode"
	"github.com/decred/dcrd/crypto/blake256"
)

// MatchIDSize defines the length in bytes of an MatchID.
const MatchIDSize = blake256.Size

// MatchID is the unique identifier for each match.
type MatchID [MatchIDSize]byte

// MatchID implements fmt.Stringer.
func (id MatchID) String() string {
	return hex.EncodeToString(id[:])
}

// Bytes returns the match ID as a []byte.
func (id MatchID) Bytes() []byte {
	return id[:]
}

// Value implements the sql/driver.Valuer interface.
func (mid MatchID) Value() (driver.Value, error) {
	return mid[:], nil // []byte
}

// Scan implements the sql.Scanner interface.
func (mid *MatchID) Scan(src interface{}) error {
	switch src := src.(type) {
	case []byte:
		copy(mid[:], src)
		return nil
		//case string:
		// case nil:
		// 	*oid = nil
		// 	return nil
	}

	return fmt.Errorf("cannot convert %T to OrderID", src)
}

var zeroMatchID MatchID

// MatchStatus represents the current negotiation step for a match.
type MatchStatus uint8

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
	// MakerRedeemed: Maker has acknowledged their audit request and broadcast
	// their redemption transaction. The DEX has validated the redemption and
	// sent the details to the taker.
	MakerRedeemed // 3
	// MatchComplete: Taker has acknowledged their audit request and broadcast
	// their redemption transaction. The DEX has validated the redemption and
	// sent the details to the maker.
	MatchComplete // 4
	//MatchRefunded // 5?
)

// String satisfies fmt.Stringer.
func (status MatchStatus) String() string {
	switch status {
	case NewlyMatched:
		return "NewlyMatched"
	case MakerSwapCast:
		return "MakerSwapCast"
	case TakerSwapCast:
		return "TakerSwapCast"
	case MakerRedeemed:
		return "MakerRedeemed"
	case MatchComplete:
		return "MatchComplete"
	}
	return "MatchStatusUnknown"
}

// MatchSide is the client's side in a match. It will be one of Maker or Taker.
type MatchSide uint8

const (
	// Maker is the order that matches out of the epoch queue.
	Maker MatchSide = iota
	// Taker is the order from the order book.
	Taker
)

func (side MatchSide) String() string {
	switch side {
	case Maker:
		return "Maker"
	case Taker:
		return "Taker"
	}
	return "UnknownMatchSide"
}

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

// EpochID contains the uniquely-identifying information for an epoch: index and
// duration.
type EpochID struct {
	Idx uint64
	Dur uint64
}

// End is the end time of the epoch.
func (e *EpochID) End() time.Time {
	return encode.UnixTimeMilli(int64((e.Idx + 1) * e.Dur))
}

// Match represents a match between two orders.
type Match struct {
	Taker    Order
	Maker    *LimitOrder
	Quantity uint64
	Rate     uint64

	// The following fields are not part of the serialization of Match.
	FeeRateBase  uint64
	FeeRateQuote uint64
	Epoch        EpochID
	Status       MatchStatus
	Sigs         Signatures
	cachedHash   MatchID
}

// A UserMatch is similar to a Match, but contains less information about the
// counter-party, and it clarifies which side the user is on. This is the
// information that might be provided to the client when they are resyncing
// their matches after a reconnect.
type UserMatch struct {
	OrderID     OrderID
	MatchID     MatchID
	Quantity    uint64
	Rate        uint64
	Address     string
	Status      MatchStatus
	Side        MatchSide
	FeeRateSwap uint64
	// TODO: include Sell bool?
}

// A constructor for a Match with Status = NewlyMatched. This is the preferred
// method of making a Match, since it pre-calculates and caches the match ID.
func newMatch(taker Order, maker *LimitOrder, qty, rate uint64, epochID EpochID) *Match {
	m := &Match{
		Taker:    taker,
		Maker:    maker,
		Quantity: qty,
		Rate:     rate,
		Epoch:    epochID,
	}
	// Pre-cache the ID.
	m.ID()
	return m
}

// ID computes the match ID and stores it for future calls.
// BLAKE256([maker order id] + [taker order id] + [match qty] + [match rate])
func (match *Match) ID() MatchID {
	if match.cachedHash != zeroMatchID {
		return match.cachedHash
	}
	b := make([]byte, 0, 2*OrderIDSize+8+8)
	b = appendOrderID(b, match.Taker)
	b = appendOrderID(b, match.Maker) // this maker and taker may only be matched once
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
	Epoch   EpochID
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
		match := newMatch(set.Taker, maker, set.Amounts[i], set.Rates[i], set.Epoch)
		matches = append(matches, match)
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

// MatchProof contains the key results of an epoch's order matching.
type MatchProof struct {
	Epoch     EpochID
	Preimages []Preimage
	Misses    []Order
	CSum      []byte
	Seed      []byte
}
