// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"fmt"
	"time"

	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/dex/lexi"
	"decred.org/dcrdex/tatanka/tanka"
)

type dbBond struct {
	*tanka.Bond
}

func (bond *dbBond) MarshalBinary() ([]byte, error) {
	const bondVer = 0
	var b encode.BuildyBytes = make([]byte, 1, 1+tanka.PeerIDLength+4+len(bond.CoinID)+8+8)
	b[0] = bondVer
	b = b.AddData(bond.PeerID[:]).
		AddData(encode.Uint32Bytes(bond.AssetID)).
		AddData(bond.CoinID).
		AddData(encode.Uint64Bytes(bond.Strength)).
		AddData(encode.Uint64Bytes(uint64(bond.Expiration.Unix())))
	return b, nil
}

func (bond *dbBond) UnmarshalBinary(b []byte) error {
	const bondVer = 0
	bond.Bond = new(tanka.Bond)
	ver, pushes, err := encode.DecodeBlob(b, 5)
	if err != nil {
		return fmt.Errorf("error decoding bond blob: %w", err)
	}
	if ver != bondVer {
		return fmt.Errorf("unknown bond version %d", ver)
	}
	if len(pushes) != 5 {
		return fmt.Errorf("unknown number of bond blob pushes %d", len(pushes))
	}
	copy(bond.PeerID[:], pushes[0])
	bond.AssetID = encode.BytesToUint32(pushes[1])
	bond.CoinID = pushes[2]
	bond.Strength = encode.BytesToUint64(pushes[3])
	bond.Expiration = time.Unix(int64(encode.BytesToUint64(pushes[4])), 0)
	return nil
}

func (d *DB) StoreBond(newBond *tanka.Bond) error {
	return d.bonds.Set(lexi.B(newBond.CoinID), &dbBond{newBond}, lexi.WithReplace())
}

func (d *DB) GetBonds(peerID tanka.PeerID) ([]*tanka.Bond, error) {
	var bonds []*tanka.Bond
	now := time.Now()
	return bonds, d.bonderIdx.Iterate(peerID[:], func(it *lexi.Iter) error {
		return it.V(func(vB []byte) error {
			var bond dbBond
			if err := bond.UnmarshalBinary(vB); err != nil {
				return fmt.Errorf("error unmarshaling bond: %w", err)
			}
			if bond.Expiration.Before(now) {
				it.Delete()
				return nil
			}
			bonds = append(bonds, bond.Bond)
			return nil
		})
	})
}
