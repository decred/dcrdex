// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
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
	var i int
	return agg, d.scoredIdx.Iterate(scored[:], func(it *lexi.Iter) error {
		if i >= tanka.MaxReputationEntries {
			return it.Delete()
		}
		if err := it.V(func(vB []byte) error {
			if len(vB) != 1 {
				return fmt.Errorf("score not a single byte. length = %d", len(vB))
			}
			agg.Score += int64(vB[0])
			return nil
		}); err != nil {
			return err
		}
		agg.Depth++
		i++
		return nil
	}, lexi.WithUpdate())
}
