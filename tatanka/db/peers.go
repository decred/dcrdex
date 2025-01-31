// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"fmt"

	"decred.org/dcrdex/tatanka/tanka"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

func (d *DB) Peer(peerID tanka.PeerID) (_ *tanka.Peer, err error) {
	p := &tanka.Peer{
		ID: peerID,
	}
	if p.PubKey, err = secp256k1.ParsePubKey(peerID[:]); err != nil {
		return nil, fmt.Errorf("ParsePubKey error: %w", err)
	}

	if p.Bonds, err = d.GetBonds(peerID); err != nil {
		return nil, fmt.Errorf("GetBonds error: %w", err)
	}

	if p.Reputation, err = d.Reputation(peerID); err != nil {
		return nil, fmt.Errorf("error getting Reputation: %w", err)
	}

	return p, nil
}
