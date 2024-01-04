// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package db

import (
	"encoding"
	"encoding/binary"
	"fmt"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/tatanka/tanka"
)

// Reputation entries with key encoded as
//    peer_id | 6_bytes_millisecond_timestamp | 32_byte_penalty_id
// Value is empty if it's a success. For penalties, the value is
//   1_byte_score_decrement | encoded_penalty_proof.

func (d *DB) Reputation(peerID tanka.PeerID) (rep *tanka.Reputation, err error) {
	rep = &tanka.Reputation{
		Points: make([]int8, 0, tanka.MaxReputationEntries),
	}
	return rep, d.reputationDB.ForEach(func(k, v []byte) error {
		if len(v) == 0 {
			// Success
			rep.Score++
			rep.Points = append(rep.Points, 1)
			return nil
		}
		rep.Score -= int16(v[0])
		rep.Points = append(rep.Points, -int8(v[0]))
		return nil
	}, WithPrefix(peerID[:]), WithReverse(), WithMaxEntries(tanka.MaxReputationEntries, true))
}

func (d *DB) RegisterSuccess(peerID tanka.PeerID, stamp time.Time, refID [32]byte) error {
	const keyLength = tanka.PeerIDLength + 6 + 32
	k := make([]byte, keyLength)
	copy(k[:tanka.PeerIDLength], peerID[:])
	stampB := make([]byte, 8)
	binary.BigEndian.PutUint64(stampB, uint64(stamp.UnixMilli()))
	copy(k[tanka.PeerIDLength:tanka.PeerIDLength+6], stampB[2:])
	copy(k[tanka.PeerIDLength+6:], refID[:])

	if err := d.reputationDB.Store(k, dex.Bytes{}); err != nil {
		return fmt.Errorf("error storing success for user %q: %v", peerID, err)
	}

	// TODO: Periodically run a nanny function to clear extra entries. Otherwise
	// entries are only deleted during the score scan. So theoretically, someone
	// could fill the db with a billion successes. Same in RegisterPenalty.

	return nil
}

func (d *DB) RegisterPenalty(peerID tanka.PeerID, stamp time.Time, penaltyID [32]byte, scoreDecrement uint8, penaltyInfo encoding.BinaryMarshaler) error {
	const keyLength = tanka.PeerIDLength + 6 + 32
	k := make([]byte, keyLength)
	copy(k[:tanka.PeerIDLength], peerID[:])
	stampB := make([]byte, 8)
	binary.BigEndian.PutUint64(stampB, uint64(stamp.UnixMilli()))
	copy(k[tanka.PeerIDLength:tanka.PeerIDLength+6], stampB[2:])
	copy(k[tanka.PeerIDLength+6:], penaltyID[:])

	b, err := penaltyInfo.MarshalBinary()
	if err != nil {
		return fmt.Errorf("error marshaling penalty info: %w", err)
	}
	v := make(dex.Bytes, 1+len(b))
	v[0] = scoreDecrement
	copy(v[1:], b)
	if err := d.reputationDB.Store(k, v); err != nil {
		return fmt.Errorf("error storing penalty: %w", err)
	}
	return nil
}
