// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package tatanka

import (
	"sync"

	"decred.org/dcrdex/tatanka/tanka"
)

// peer represents a locally connected client.
type peer struct {
	tanka.Sender

	mtx         sync.RWMutex
	*tanka.Peer // Bonds and Reputation protected by mtx
	rrs         map[tanka.PeerID]*tanka.Reputation
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
