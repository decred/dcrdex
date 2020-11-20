// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package auth

import (
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

const (
	cancelThreshWindow = 100 // spec
	scoringMatchLimit  = 60  // last N matches (success or at-fault fail) to be considered in swap inaction scoring
	scoringOrderLimit  = 40  // last N orders to be considered in preimage miss scoring

	// These coefficients are used to compute a user's swap limit adjustment via
	// UserOrderLimitAdjustment based on the cumulative amounts in the different
	// match outcomes.
	successWeight    int64 = 3
	stuckLongWeight  int64 = -5
	stuckShortWeight int64 = -3
	spoofedWeight    int64 = -1
)

// violation badness
const (
	// preimage miss
	preimageMissScore = 2 // book spoof, no match, no stuck funds

	// failure to act violations
	noSwapAsMakerScore   = 4  // book spoof, match with taker order affected, no stuck funds
	noSwapAsTakerScore   = 11 // maker has contract stuck for 20 hrs
	noRedeemAsMakerScore = 7  // taker has contract stuck for 8 hrs
	noRedeemAsTakerScore = 1  // just dumb, counterparty not inconvenienced

	successScore = -1 // offsets the violations

	defaultBanScore = 20
)

// Violation represents a specific infraction. For example, not broadcasting a
// swap contract transaction by the deadline as the maker.
type Violation int32

const (
	ViolationInvalid Violation = iota - 2
	ViolationForgiven
	ViolationSwapSuccess
	ViolationPreimageMiss
	ViolationNoSwapAsMaker
	ViolationNoSwapAsTaker
	ViolationNoRedeemAsMaker
	ViolationNoRedeemAsTaker
)

var violations = map[Violation]struct {
	score int32
	desc  string
}{
	ViolationSwapSuccess:     {successScore, "swap success"},
	ViolationForgiven:        {-1, "forgiveness"},
	ViolationPreimageMiss:    {preimageMissScore, "preimage miss"},
	ViolationNoSwapAsMaker:   {noSwapAsMakerScore, "no swap as maker"},
	ViolationNoSwapAsTaker:   {noSwapAsTakerScore, "no swap as taker"},
	ViolationNoRedeemAsMaker: {noRedeemAsMakerScore, "no redeem as maker"},
	ViolationNoRedeemAsTaker: {noRedeemAsTakerScore, "no redeem as taker"},
	ViolationInvalid:         {0, "invalid violation"},
}

// Score returns the Violation's score, which is a representation of the
// relative severity of the infraction.
func (v Violation) Score() int32 {
	return violations[v].score
}

// String returns a description of the Violation.
func (v Violation) String() string {
	return violations[v].desc
}

// NoActionStep is the action that the user failed to take. This is used to
// define valid inputs to the Inaction method.
type NoActionStep uint8

const (
	SwapSuccess NoActionStep = iota // success included for accounting purposes
	NoSwapAsMaker
	NoSwapAsTaker
	NoRedeemAsMaker
	NoRedeemAsTaker
)

// Violation returns the corresponding Violation for the misstep represented by
// the NoActionStep.
func (step NoActionStep) Violation() Violation {
	switch step {
	case SwapSuccess:
		return ViolationSwapSuccess
	case NoSwapAsMaker:
		return ViolationNoSwapAsMaker
	case NoSwapAsTaker:
		return ViolationNoSwapAsTaker
	case NoRedeemAsMaker:
		return ViolationNoRedeemAsMaker
	case NoRedeemAsTaker:
		return ViolationNoRedeemAsTaker
	default:
		return ViolationInvalid
	}
}

// String returns the description of the NoActionStep's corresponding Violation.
func (step NoActionStep) String() string {
	return step.Violation().String()
}

// Reputation is the user's reputation, which takes into account recent orders
// and matches.
type Reputation struct {
	initTakerLotLimit int64
	absTakerLotLimit  int64
	matches           *latestMatchOutcomes
	ords              *latestPreimageOutcomes
}

// NewReputation is the constructor for a *Reputation.
func NewReputation(initTakerLotLimit, absTakerLotLimit int64) *Reputation {
	return &Reputation{
		initTakerLotLimit: initTakerLotLimit,
		absTakerLotLimit:  absTakerLotLimit,
		matches:           newLatestMatchOutcomes(scoringMatchLimit),
		ords:              newLatestPreimageOutcomes(scoringOrderLimit),
	}
}

func (r *Reputation) mktSwapAmounts(base, quote uint32) *SwapAmounts {
	return r.matches.mktSwapAmounts(base, quote)
}

// userScore computes the user score from the user's recent match outcomes and
// preimage history. This must be called with the violationMtx locked.
func (r *Reputation) userScore() (score int32) {
	for v, count := range r.matches.binViolations() {
		score += v.Score() * int32(count)
	}
	score += ViolationPreimageMiss.Score() * r.ords.misses()
	return
}

// registerMatchOutcome registers the outcome from a match.
func (r *Reputation) registerMatchOutcome(violation Violation, base, quote uint32, matchID order.MatchID, value uint64, refTime time.Time) {
	r.matches.add(&matchOutcome{
		time:    encode.UnixMilli(refTime),
		mid:     matchID,
		outcome: violation,
		value:   value,
		base:    base,
		quote:   quote,
	})
}

// registerMatchOutcome registers the outcome from a preimage request.
func (r *Reputation) registerPreimageOutcome(miss bool, oid order.OrderID, refTime time.Time) {
	r.ords.add(&preimageOutcome{
		time: encode.UnixMilli(refTime),
		oid:  oid,
		miss: miss,
	})
}

// userSettlingLimit returns a user's settling amount limit for the given market
// in units of the base asset. The limit may be negative for accounts with poor
// swap history.
func (r *Reputation) userSettlingLimit(base, quote uint32, lotSize uint64) int64 {
	currentLotSize := int64(lotSize)
	start := currentLotSize * r.initTakerLotLimit

	sa := r.mktSwapAmounts(base, quote)

	limit := start + sa.Swapped*successWeight + sa.StuckLong*stuckLongWeight + sa.StuckShort*stuckShortWeight + sa.Spoofed*spoofedWeight
	if limit/currentLotSize >= r.absTakerLotLimit {
		limit = r.absTakerLotLimit * currentLotSize
	}
	return limit
}
