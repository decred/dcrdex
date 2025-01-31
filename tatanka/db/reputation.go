// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"encoding/binary"
	"fmt"
	"time"

	"decred.org/dcrdex/dex/lexi"
	"decred.org/dcrdex/tatanka/tanka"
)

type dbScore struct {
	scorer tanka.PeerID
	scored tanka.PeerID
	score  int8
	stamp  time.Time
}

func (s *dbScore) MarshalBinary() ([]byte, error) {
	return []byte{byte(s.score)}, nil
}

func (d *DB) SetScore(scored, scorer tanka.PeerID, score int8, stamp time.Time) error {
	k := append(scored[:], scorer[:]...)
	s := &dbScore{
		scorer: scorer,
		scored: scored,
		score:  score,
		stamp:  stamp,
	}
	return d.scores.Set(lexi.B(k), s, lexi.WithReplace())
}

func (d *DB) Reputation(scored tanka.PeerID) (*tanka.Reputation, error) {
	agg := new(tanka.Reputation)
	var s dbScore
	var i int
	return agg, d.scoredIdx.Iterate(scored[:], func(it *lexi.Iter) error {
		if i >= tanka.MaxReputationEntries {
			return it.Delete()
		}
		k, err := it.K()
		if err != nil {
			return fmt.Errorf("error getting score key: %w", err)
		}
		if len(k) != 2*tanka.PeerIDLength {
			return fmt.Errorf("wrong score key length: %d != %d", len(k), 2*tanka.PeerIDLength)
		}
		copy(s.scored[:], k)
		copy(s.scorer[:], k[tanka.PeerIDLength:])
		if err := it.V(func(vB []byte) error {
			if len(vB) != 1 {
				return fmt.Errorf("score not a single byte. length = %d", len(vB))
			}
			s.score = int8(vB[0])
			return nil
		}); err != nil {
			return err
		}
		if err := it.Entry(func(idxB []byte) error {
			if len(idxB) != tanka.PeerIDLength+8 {
				return fmt.Errorf("wrong score-stamp index length %d", len(idxB))
			}
			s.stamp = time.UnixMilli(int64(binary.BigEndian.Uint64(idxB[tanka.PeerIDLength:])))
			return nil
		}); err != nil {
			return err
		}

		agg.Score += int64(s.score)
		agg.Depth++
		i++
		return nil
	}, lexi.WithUpdate())
}
