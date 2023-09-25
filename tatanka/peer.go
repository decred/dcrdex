// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package tatanka

import (
	"math"
	"sync"

	"decred.org/dcrdex/tatanka/mj"
	"decred.org/dcrdex/tatanka/tanka"
)

// peer represents a locally connected client.
type peer struct {
	tanka.Sender

	mtx         sync.RWMutex
	*tanka.Peer // Bonds and Reputation protected by mtx
	rrs         map[tanka.PeerID]*mj.RemoteReputation
}

// score calculates the peer's reputation score.
func (p *peer) score() int16 {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	// If we have registered enough points ourselves, just use our score.
	if len(p.Reputation.Points) == tanka.MaxReputationEntries {
		return p.Reputation.Score
	}
	// Create a composite score. This is not an average. If we have 90 or the
	// max 100 sepearate point entries, our score is weighted as 90%. The other
	// 10% is taken from a poll of peers, weighted according to the number of
	// points that they report.
	myScoreRatio := float64(len(p.Reputation.Points)) / tanka.MaxReputationEntries
	remainingScoreRatio := (tanka.MaxReputationEntries - float64(len(p.Reputation.Points))) / tanka.MaxReputationEntries
	var weight int64
	var ptCount uint32
	for _, r := range p.rrs {
		if r == nil {
			continue
		}
		weight += int64(r.Score) * int64(r.NumPts)
		ptCount += uint32(r.NumPts)
	}

	remoteScore := float64(weight) / float64(ptCount)
	score := int64(math.Round(float64(p.Reputation.Score)*myScoreRatio + remoteScore*remainingScoreRatio))
	if score > math.MaxUint16 {
		score = math.MaxInt64
	}
	if score < -int64(math.MaxInt16) {
		score = -int64(math.MaxInt16)
	}
	return int16(score)
}

// banned checks whether the peer is banned.
func (p *peer) banned() bool {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	// NEED TO CONSIDER *RemoteReputations....
	tier := calcTier(p.Reputation, p.BondTier())
	return tier <= 0
}

// bondTier is the peer's current bonded tier.
func (p *peer) bondTier() uint64 {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	return p.BondTier()
}

// updateBonds updates the peers bond set.
func (p *peer) updateBonds(bonds []*tanka.Bond) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.Bonds = bonds
}

type client struct {
	*peer
}
