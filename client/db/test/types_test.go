package dbtest

import (
	"testing"
	"time"

	"decred.org/dcrdex/client/db"
)

func TestAccountInfo(t *testing.T) {
	spins := 10000
	ais := make([]*db.AccountInfo, 0, spins)
	nTimes(spins, func(int) { ais = append(ais, RandomAccountInfo()) })
	tStart := time.Now()
	nTimes(spins, func(i int) {
		ai := RandomAccountInfo()
		aiB := ai.Encode()
		reAI, err := db.DecodeAccountInfo(aiB)
		if err != nil {
			t.Fatalf("error decoding AccountInfo: %v", err)
		}
		MustCompareAccountInfo(t, ai, reAI)
	})
	t.Logf("encoded, decoded, and compared %d AccountInfo in %d ms", spins, time.Since(tStart)/time.Millisecond)
}

func TestMatchProof(t *testing.T) {
	spins := 10000
	proofs := make([]*db.MatchProof, 0, spins)
	// Generate proofs with an average of 20% sparsity. Empty fields should not
	// affect accurate encoding/decoding.
	nTimes(spins, func(int) { proofs = append(proofs, RandomMatchProof(0.4)) })
	tStart := time.Now()
	nTimes(spins, func(i int) {
		proof := proofs[i]
		proofB := proof.Encode()
		reProof, err := db.DecodeMatchProof(proofB)
		if err != nil {
			t.Fatalf("match decode error: %v", err)
		}
		MustCompareMatchProof(t, proof, reProof)
	})
	t.Logf("encoded, decoded, and compared %d MatchProof in %d ms", spins, time.Since(tStart)/time.Millisecond)
}

func TestOrderProof(t *testing.T) {
	spins := 10000
	proofs := make([]*db.OrderProof, 0, spins)
	nTimes(spins, func(int) { proofs = append(proofs, &db.OrderProof{DEXSig: randBytes(73)}) })
	tStart := time.Now()
	nTimes(spins, func(i int) {
		proof := proofs[i]
		proofB := proof.Encode()
		reProof, err := db.DecodeOrderProof(proofB)
		if err != nil {
			t.Fatalf("decode error: %v", err)
		}
		MustCompareOrderProof(t, proof, reProof)
	})
	t.Logf("encoded, decoded, and compared %d OrderProof in %d ms", spins, time.Since(tStart)/time.Millisecond)
}

func nTimes(n int, f func(int)) {
	for i := 0; i < n; i++ {
		f(i)
	}
}
