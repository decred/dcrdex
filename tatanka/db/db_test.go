package db

import (
	"encoding/binary"
	"os"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/encode"
	"decred.org/dcrdex/tatanka/tanka"
)

func TestReputation(t *testing.T) {
	dir, _ := os.MkdirTemp("", "")
	defer os.RemoveAll(dir)

	log := dex.StdOutLogger("T", dex.LevelTrace)

	d, err := New(dir, log)
	if err != nil {
		t.Fatalf("New error: %v", err)
	}

	var peerID tanka.PeerID
	peerID[0] = 0x1
	peerID[tanka.PeerIDLength-1] = 0x1

	rep, err := d.Reputation(peerID)
	if err != nil {
		t.Fatalf("Error getting first reputation: %v", err)
	}
	if rep.Score != 0 {
		t.Fatalf("non-zero initial score %d", rep.Score)
	}

	var n, penalties, decrement uint16
	success := func() {
		var refID [32]byte
		copy(refID[:], encode.RandomBytes(32))
		if err := d.RegisterSuccess(peerID, time.Now(), refID); err != nil {
			t.Fatalf("RegisterSuccess error: %v", err)
		}
		n++
	}
	penalize := func(dec uint8) {
		penalties++
		decrement += uint16(dec)
		penaltiesB := make([]byte, 2)
		binary.BigEndian.PutUint16(penaltiesB, penalties)
		var penaltyID [32]byte
		copy(penaltyID[:], penaltiesB)
		penaltyProof := make(dex.Bytes, 2)
		if err := d.RegisterPenalty(peerID, time.Now(), penaltyID, dec, penaltyProof); err != nil {
			t.Fatalf("RegisterPenalty error: %v", err)
		}
	}
	checkScore := func(expScore int16) {
		rep, err := d.Reputation(peerID)
		if err != nil {
			t.Fatalf("Reputation error: %v", err)
		}
		if rep.Score != expScore {
			t.Fatalf("wanted score %d, got %d", expScore, rep.Score)
		}
	}

	success()
	checkScore(1)

	var someoneElse tanka.PeerID
	us := peerID
	peerID = someoneElse
	success()
	peerID = us
	checkScore(1)

	for n < tanka.MaxReputationEntries*2 {
		success()
	}
	checkScore(tanka.MaxReputationEntries)

	penalize(10)
	checkScore((tanka.MaxReputationEntries - 1) - 10)
}
