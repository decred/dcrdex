package db

import (
	"bytes"
	"os"
	"testing"
	"time"

	"decred.org/dcrdex/dex"
	"decred.org/dcrdex/dex/lexi"
	"decred.org/dcrdex/tatanka/tanka"
)

func tNewDB() (*DB, func()) {
	tempDir, _ := os.MkdirTemp("", "")
	db, err := New(tempDir, dex.StdOutLogger("T", dex.LevelInfo))
	if err != nil {
		panic(err.Error())
	}
	return db, func() {
		os.RemoveAll(tempDir)
	}
}

func newBond(peer tanka.PeerID, n byte, timeOffset ...time.Duration) *tanka.Bond {
	expiration := time.Now().Add(time.Minute)
	if len(timeOffset) > 0 {
		expiration = time.Now().Add(timeOffset[0])
	}
	return &tanka.Bond{
		PeerID:     peer,
		AssetID:    uint32(n),
		CoinID:     []byte{n},
		Strength:   uint64(n),
		Expiration: expiration,
	}
}

func TestBonds(t *testing.T) {
	// Testing basic bond insertion and retrieval, and binary marshaling.
	db, shutdown := tNewDB()
	defer shutdown()

	peer := tanka.PeerID{0x01}
	b1 := newBond(peer, 1)
	if err := db.StoreBond(b1); err != nil {
		t.Fatalf("StoreBond error: %v", err)
	}
	bs, err := db.GetBonds(peer)
	if err != nil {
		t.Fatalf("GetBonds error: %v", err)
	}
	if len(bs) != 1 {
		t.Fatalf("Expected 1 bond, got %d", len(bs))
	}
	reB := bs[0]
	if b1.PeerID != reB.PeerID {
		t.Fatalf("Wrong peer ID. %s != %s", b1.PeerID, reB.PeerID)
	}
	if b1.AssetID != reB.AssetID {
		t.Fatalf("Wrong asset ID. %d != %d", b1.AssetID, reB.AssetID)
	}
	if !bytes.Equal(b1.CoinID, reB.CoinID) {
		t.Fatalf("Wrong coin ID, %v != %v", b1.CoinID, reB.CoinID)
	}
	if b1.Strength != reB.Strength {
		t.Fatalf("Wrong strength. %d != %d", b1.Strength, reB.Strength)
	}
	if b1.Expiration.Unix() != reB.Expiration.Unix() {
		t.Fatalf("Wrong time. %d != %d", b1.Expiration.Unix(), reB.Expiration.Unix())
	}

	// Works with > 1 bond too
	db.StoreBond(newBond(peer, 2))
	bs, _ = db.GetBonds(peer)
	if len(bs) != 2 {
		t.Fatalf("Expected 2 bonds, got %d", len(bs))
	}

	// Expired bonds are not returned
	db.StoreBond(newBond(peer, 3, -time.Second))
	bs, _ = db.GetBonds(peer)
	if len(bs) != 2 {
		t.Fatalf("Expected 2 bonds after pruning, got %d", len(bs))
	}
}

func TestPruneBonds(t *testing.T) {
	db, shutdown := tNewDB()
	defer shutdown()

	peer := tanka.PeerID{0x01}
	db.StoreBond(newBond(peer, 0))
	db.StoreBond(newBond(peer, 1))
	db.StoreBond(newBond(peer, 2))
	db.StoreBond(newBond(peer, 3, -time.Second))
	db.StoreBond(newBond(peer, 4, -time.Second))
	db.StoreBond(newBond(peer, 5, -time.Second))
	// Make sure they're all there.
	var n int
	db.bonderIdx.Iterate(nil, func(it *lexi.Iter) error {
		n++
		return nil
	})
	if n != 6 {
		t.Fatalf("Where'd the bonds go?")
	}
	db.pruneOldBonds()
	n = 0
	db.bonderIdx.Iterate(nil, func(it *lexi.Iter) error {
		n++
		return nil
	})
	if n != 3 {
		t.Fatalf("Expected 3 bonds, got %d", n)
	}
}

func TestReputation(t *testing.T) {
	db, shutdown := tNewDB()
	defer shutdown()

	var scored tanka.PeerID
	const outdatedN = 5 // won't be included in score. Will be deleted.
	// Note: If MaxReputationEntries is increased to > 122, this won't work
	// any more.
	for i := 0; i < tanka.MaxReputationEntries+outdatedN; i++ {
		if err := db.SetScore(scored, tanka.PeerID{byte(i + 1)}, int8(i), time.Now().Add(-time.Duration(i)*time.Second)); err != nil {
			t.Fatalf("SetScore(%d) error: %v", i, err)
		}
	}
	// Sum of positive integers from 1 to N is (N(N+1))/2
	var scoreMax int64 = tanka.MaxReputationEntries - 1 // We started with 0.
	var expAggScore int64 = (scoreMax * (scoreMax + 1)) / 2
	rep, err := db.Reputation(scored)
	if err != nil {
		t.Fatalf("Reputation error: %v", err)
	}
	if rep.Score != expAggScore {
		t.Fatalf("Wrong aggregate score. Expected %d, got %d", expAggScore, rep.Score)
	}
	if rep.Depth != tanka.MaxReputationEntries {
		t.Fatalf("Wrong aggregate depth. Expected %d, got %d", tanka.MaxReputationEntries, rep.Depth)
	}
	// Max sure old scores were deleted.
	var n int
	db.scoredIdx.Iterate(scored[:], func(it *lexi.Iter) error {
		n++
		return nil
	})
	if n != tanka.MaxReputationEntries {
		t.Fatalf("Wrong number of remaining entries. Expected %d, got %d", tanka.MaxReputationEntries, n)
	}
}
