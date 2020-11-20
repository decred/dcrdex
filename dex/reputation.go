// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package dex

import (
	"bytes"
	"math"
	"sort"
	"sync"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/order"
)

const (
	CancelThreshWindow = 100 // spec
	ScoringMatchLimit  = 60  // last N matches (success or at-fault fail) to be considered in swap inaction scoring
	ScoringOrderLimit  = 40  // last N orders to be considered in preimage miss scoring

	// preimage miss
	PreimageMissScore = -2 // book spoof, no match, no stuck funds

	// failure to act violations
	NoSwapAsMakerScore   = -4  // book spoof, match with taker order affected, no stuck funds
	NoSwapAsTakerScore   = -11 // maker has contract stuck for 20 hrs
	NoRedeemAsMakerScore = -7  // taker has contract stuck for 8 hrs
	NoRedeemAsTakerScore = -1  // just dumb, counterparty not inconvenienced

	SuccessScore = 1 // offsets the violations

	DefaultBaselineScore = 20
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
	ViolationSwapSuccess:     {SuccessScore, "swap success"},
	ViolationForgiven:        {-1, "forgiveness"},
	ViolationPreimageMiss:    {PreimageMissScore, "preimage miss"},
	ViolationNoSwapAsMaker:   {NoSwapAsMakerScore, "no swap as maker"},
	ViolationNoSwapAsTaker:   {NoSwapAsTakerScore, "no swap as taker"},
	ViolationNoRedeemAsMaker: {NoRedeemAsMakerScore, "no redeem as maker"},
	ViolationNoRedeemAsTaker: {NoRedeemAsTakerScore, "no redeem as taker"},
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

// ViolationFromMatchStatus converts a final MatchStatus to a Violation.
func ViolationFromMatchStatus(status order.MatchStatus) Violation {
	switch status {
	case order.NewlyMatched:
		return ViolationNoSwapAsMaker
	case order.MakerSwapCast:
		return ViolationNoSwapAsTaker
	case order.TakerSwapCast:
		return ViolationNoRedeemAsMaker
	case order.MakerRedeemed:
		return ViolationNoRedeemAsTaker
	case order.MatchComplete:
		return ViolationSwapSuccess // should be caught by Fail==false
	}
	return ViolationInvalid
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
	baseline int32
	matches  *latestMatchOutcomes
	ords     *latestPreimageOutcomes
}

// NewReputation is the constructor for a *Reputation.
func NewReputation(baseline int32) *Reputation {
	return &Reputation{
		baseline: baseline,
		matches:  newLatestMatchOutcomes(ScoringMatchLimit),
		ords:     newLatestPreimageOutcomes(ScoringOrderLimit),
	}
}

// RawScore is the score in the internal scoring system. RawScore will be on the
// range [0, maxScore]. The RawScore is computed from the user's recent match
// outcomes and preimage history.
func (r *Reputation) RawScore() (score int32) {
	score = r.baseline
	for v, count := range r.matches.binViolations() {
		score += v.Score() * int32(count)
	}
	score += r.ords.score()
	return
}

// Score is the normalized user score, and will range from < 0 (for penalized
// users) to max 100.
func (r *Reputation) Score() int {
	return int(math.Round(float64(r.RawScore()) / float64(r.maxScore()) * 100))
}

// Privilege is the amount of privilege the user has, and is computed as
// (rawScore - baseline) / (maxScore - baseline), and will be on the range
// (0.0, 1.0).
func (r *Reputation) Privilege() float64 {
	rawScore := r.RawScore()
	if rawScore < r.baseline {
		return 0
	}
	return float64(rawScore-r.baseline) / float64(r.maxScore()-r.baseline)
}

// RegisterMatchOutcome registers the outcome from a match.
func (r *Reputation) RegisterMatchOutcome(violation Violation, base, quote uint32, matchID order.MatchID, value uint64, refTime time.Time) {
	r.matches.add(&matchOutcome{
		time:    encode.UnixMilli(refTime),
		mid:     matchID,
		outcome: violation,
		value:   value,
		base:    base,
		quote:   quote,
	})
}

// RegisterPreimageOutcome registers the outcome from a preimage request.
func (r *Reputation) RegisterPreimageOutcome(miss bool, oid order.OrderID, refTime time.Time) {
	r.ords.add(&preimageOutcome{
		time: encode.UnixMilli(refTime),
		oid:  oid,
		miss: miss,
	})
}

func (r *Reputation) maxScore() int32 {
	return r.baseline + ScoringMatchLimit + ScoringOrderLimit
}

type matchOutcome struct {
	// sorting is done by time and match ID
	time int64
	mid  order.MatchID

	// match outcome and value
	outcome     Violation
	base, quote uint32 // market
	value       uint64
}

func lessByTimeThenMID(ti, tj *matchOutcome) bool {
	if ti.time == tj.time {
		return bytes.Compare(ti.mid[:], tj.mid[:]) < 0 // ascending (smaller ID first)
	}
	return ti.time < tj.time // ascending (newest last in slice)
}

type latestMatchOutcomes struct {
	mtx      sync.Mutex
	cap      int16
	outcomes []*matchOutcome
}

func newLatestMatchOutcomes(cap int16) *latestMatchOutcomes {
	return &latestMatchOutcomes{
		cap:      cap,
		outcomes: make([]*matchOutcome, 0, cap+1), // cap+1 since an old item is popped *after* a new one is pushed
	}
}

func (la *latestMatchOutcomes) add(mo *matchOutcome) {
	la.mtx.Lock()
	defer la.mtx.Unlock()

	// Use sort.Search and insert it at the right spot.
	n := len(la.outcomes)
	i := sort.Search(n, func(i int) bool {
		return lessByTimeThenMID(la.outcomes[n-1-i], mo)
	})
	if i == int(la.cap) /* i == n && n == int(la.cap) */ {
		// The new one is the oldest/smallest, but already at capacity.
		return
	}
	// Insert at proper location.
	i = n - i // i-1 is first location that stays
	la.outcomes = append(la.outcomes[:i], append([]*matchOutcome{mo}, la.outcomes[i:]...)...)

	// Pop one stamped if the slice was at capacity prior to pushing the new one.
	if len(la.outcomes) > int(la.cap) {
		// pop front, the oldest stamped
		la.outcomes[0] = nil // avoid memory leak
		la.outcomes = la.outcomes[1:]
	}
}

func (la *latestMatchOutcomes) binViolations() map[Violation]int64 {
	bins := make(map[Violation]int64)
	for _, mo := range la.outcomes {
		bins[mo.outcome]++
	}
	return bins
}

type preimageOutcome struct {
	time int64
	oid  order.OrderID
	miss bool
}

func lessByTimeThenOID(ti, tj *preimageOutcome) bool {
	if ti.time == tj.time {
		return bytes.Compare(ti.oid[:], tj.oid[:]) < 0 // ascending (smaller ID first)
	}
	return ti.time < tj.time // ascending (newest last in slice)
}

type latestPreimageOutcomes struct {
	mtx      sync.Mutex
	cap      int16
	outcomes []*preimageOutcome
}

func newLatestPreimageOutcomes(cap int16) *latestPreimageOutcomes {
	return &latestPreimageOutcomes{
		cap:      cap,
		outcomes: make([]*preimageOutcome, 0, cap+1), // cap+1 since an old item is popped *after* a new one is pushed
	}
}

func (la *latestPreimageOutcomes) add(po *preimageOutcome) {
	la.mtx.Lock()
	defer la.mtx.Unlock()

	// Use sort.Search and insert it at the right spot.
	n := len(la.outcomes)
	i := sort.Search(n, func(i int) bool {
		return lessByTimeThenOID(la.outcomes[n-1-i], po)
	})
	if i == int(la.cap) /* i == n && n == int(la.cap) */ {
		// The new one is the oldest/smallest, but already at capacity.
		return
	}
	// Insert at proper location.
	i = n - i // i-1 is first location that stays
	la.outcomes = append(la.outcomes[:i], append([]*preimageOutcome{po}, la.outcomes[i:]...)...)

	// Pop one stamped if the slice was at capacity prior to pushing the new one.
	if len(la.outcomes) > int(la.cap) {
		// pop front, the oldest stamped
		la.outcomes[0] = nil // avoid memory leak
		la.outcomes = la.outcomes[1:]
	}
}

func (la *latestPreimageOutcomes) score() int32 {
	var misses int
	n := len(la.outcomes)
	for _, th := range la.outcomes {
		if th.miss {
			misses++
		}
	}
	return int32((n-misses)*SuccessScore + misses*PreimageMissScore)
}
