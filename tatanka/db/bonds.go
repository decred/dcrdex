// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"encoding/json"
	"fmt"
	"time"

	"decred.org/dcrdex/tatanka/tanka"
)

type jsonCoder struct {
	thing interface{}
}

func newJSON(thing interface{}) *jsonCoder {
	return &jsonCoder{thing}
}

func (p *jsonCoder) MarshalBinary() ([]byte, error) {
	return json.Marshal(p.thing)
}

func (p *jsonCoder) UnmarshalBinary(b []byte) error {
	return json.Unmarshal(b, p.thing)
}

func (d *DB) StoreBond(newBond *tanka.Bond) (goodBonds []*tanka.Bond, err error) {
	var existingBonds []*tanka.Bond
	if _, err = d.bondsDB.Get(newBond.PeerID[:], newJSON(existingBonds)); err != nil {
		return nil, fmt.Errorf("error reading bonds db: %w", err)
	}

	for _, b := range existingBonds {
		if time.Now().After(b.Expiration) {
			continue
		}
		goodBonds = append(goodBonds, b)
	}

	goodBonds = append(goodBonds, newBond)
	return goodBonds, d.bondsDB.Store(newBond.PeerID[:], newJSON(goodBonds))
}

func (d *DB) GetBonds(peerID tanka.PeerID) ([]*tanka.Bond, error) {
	var existingBonds []*tanka.Bond
	if _, err := d.bondsDB.Get(peerID[:], newJSON(&existingBonds)); err != nil {
		return nil, fmt.Errorf("error reading bonds db: %w", err)
	}

	var goodBonds []*tanka.Bond
	for _, b := range existingBonds {
		if time.Now().After(b.Expiration) {
			continue
		}
		goodBonds = append(goodBonds, b)
	}

	if len(goodBonds) != len(existingBonds) {
		if err := d.bondsDB.Store(peerID[:], newJSON(goodBonds)); err != nil {
			return nil, fmt.Errorf("error storing bonds after pruning: %v", err)
		}
	}

	return goodBonds, nil
}
